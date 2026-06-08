package scheduler

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/port-agv/routing/internal/graph"
	"github.com/port-agv/routing/internal/kinematics"
	"github.com/port-agv/routing/internal/mqtt"
	"github.com/port-agv/routing/internal/router"
)

type TaskState int

const (
	TaskStatePlanned TaskState = iota
	TaskStateDispatched
	TaskStateRerouting
	TaskStateCompleted
	TaskStateFailed
)

type ScheduledTask struct {
	TaskID       string
	ContainerID  string
	AgvID        string
	YardNode     string
	QuaysideNode string
	Priority     int32
	State        TaskState
	Route        *TWRouteResult
	Frames       []router.ControlFrame
	DispatchTime float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	RerouteCount int
}

type DispatchResult struct {
	Success         bool
	Message         string
	Route           *TWRouteResult
	Frames          []router.ControlFrame
	RouteID         string
	TotalTime       float64
	TotalDist       float64
	Rerouted        bool
	RerouteVictim   string
	KinematicResult *kinematics.InterceptionResult
}

type Scheduler struct {
	graph          *graph.Digraph
	twTable        *TimeWindowTable
	tracker        *AGVTracker
	mqttClient     *mqtt.Client
	safetyGateway  *kinematics.SafetyGateway

	mu          sync.Mutex
	tasks       map[string]*ScheduledTask
	agvTasks    map[string]string
	priorities  map[string]int32
	taskCount   atomic.Int64
	rerouteCount atomic.Int64

	MaxWaitTime      float64
	DeadlockCheck    bool
	RerouteLimit     int
}

func NewScheduler(g *graph.Digraph, mqttClient *mqtt.Client) *Scheduler {
	sg := kinematics.NewSafetyGateway()
	return &Scheduler{
		graph:         g,
		twTable:       NewTimeWindowTable(),
		tracker:       NewAGVTracker(),
		mqttClient:    mqttClient,
		safetyGateway: sg,
		tasks:         make(map[string]*ScheduledTask),
		agvTasks:      make(map[string]string),
		priorities:    make(map[string]int32),
		MaxWaitTime:   DefaultMaxWaitTime,
		DeadlockCheck: true,
		RerouteLimit:  3,
	}
}

func (s *Scheduler) SafetyGateway() *kinematics.SafetyGateway {
	return s.safetyGateway
}

func (s *Scheduler) DispatchTask(taskID, containerID, agvID, yardNode, quayNode string, priority int32, deadlineUnix int64) *DispatchResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("[Scheduler] DispatchTask: task=%s agv=%s yard=%s quay=%s prio=%d",
		taskID, agvID, yardNode, quayNode, priority)

	s.priorities[agvID] = priority

	now := float64(time.Now().UnixMilli()) / 1000.0

	var startTime float64
	if pos, ok := s.tracker.Get(agvID); ok && pos.CurrentNode != "" {
		startTime = now + 2.0
	} else {
		startTime = now
	}

	route := TimeWindowDijkstra(
		s.graph, s.twTable,
		yardNode, quayNode,
		startTime, agvID, priority,
		s.MaxWaitTime,
	)

	if route == nil {
		fallback := s.tryRerouteForNewTask(taskID, agvID, yardNode, quayNode, priority, startTime)
		if fallback != nil {
			return fallback
		}
		return &DispatchResult{
			Success: false,
			Message: fmt.Sprintf("no conflict-free route from %s to %s within max wait %.0fs",
				yardNode, quayNode, s.MaxWaitTime),
		}
	}

	if s.DeadlockCheck && !s.isSafeReservation(route.Reservations) {
		log.Printf("[Scheduler] Deadlock risk detected for task=%s, attempting avoidance", taskID)
		s.twTable.ReleaseAllByAGV(agvID)

		avoidRoute := s.findAvoidanceRoute(yardNode, quayNode, startTime, agvID, priority)
		if avoidRoute != nil {
			route = avoidRoute
		} else {
			return &DispatchResult{
				Success: false,
				Message: "deadlock risk detected and no avoidance route available",
			}
		}
	}

	if !s.twTable.ReserveBatch(route.Reservations) {
		return &DispatchResult{
			Success: false,
			Message: "failed to reserve time windows (concurrent conflict)",
		}
	}

	basicRoute := router.ConvertTWRouteToRouteResult(route)
	frames := router.DecomposePathWithWaits(basicRoute, route, agvID)

	var kinematicResult *kinematics.InterceptionResult
	if s.safetyGateway != nil {
		kinematicResult = s.safetyGateway.Intercept(containerID)
		if kinematicResult != nil && kinematicResult.DegradedSpeeds != nil && kinematicResult.DegradedSpeeds.IsDegraded {
			log.Printf("[Scheduler] Kinematic degradation for container=%s: factor=%.2f reason=%s",
				containerID, kinematicResult.DegradedSpeeds.SpeedReductionFactor, kinematicResult.Reason)
			frames = s.safetyGateway.ApplySpeedDegradation(frames, kinematicResult.DegradedSpeeds)
		}
	}

	task := &ScheduledTask{
		TaskID:       taskID,
		ContainerID:  containerID,
		AgvID:        agvID,
		YardNode:     yardNode,
		QuaysideNode: quayNode,
		Priority:     priority,
		State:        TaskStateDispatched,
		Route:        route,
		Frames:       frames,
		DispatchTime: startTime,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	s.tasks[taskID] = task
	s.agvTasks[agvID] = taskID
	s.taskCount.Add(1)

	if s.mqttClient != nil && s.mqttClient.IsConnected() {
		if err := s.mqttClient.PublishControlFrames(agvID, frames); err != nil {
			log.Printf("[Scheduler] MQTT publish failed for AGV %s: %v", agvID, err)
			s.twTable.ReleaseAllByAGV(agvID)
			task.State = TaskStateFailed
			return &DispatchResult{
				Success: false,
				Message: fmt.Sprintf("MQTT publish failed: %v", err),
			}
		}
		task.State = TaskStateDispatched
	}

	return &DispatchResult{
		Success:         true,
		Message:         "task dispatched with time-window reservations",
		Route:           route,
		Frames:          frames,
		RouteID:         fmt.Sprintf("tw-route-%s-%d", taskID, time.Now().UnixMilli()),
		TotalTime:       route.TotalTime,
		TotalDist:       route.TotalDistance,
		KinematicResult: kinematicResult,
	}
}

func (s *Scheduler) tryRerouteForNewTask(taskID, agvID, yardNode, quayNode string, priority int32, startTime float64) *DispatchResult {
	wfg := BuildWaitForGraph(s.twTable)
	cycle := wfg.DetectCycle()
	if cycle == nil {
		return nil
	}

	selector := NewVictimSelector(s.priorities)
	victim := selector.SelectVictim(cycle)
	if victim == "" || victim == agvID {
		return nil
	}

	log.Printf("[Scheduler] Rerouting victim AGV %s to make room for task=%s", victim, taskID)
	s.rerouteAGV(victim)

	route := TimeWindowDijkstra(
		s.graph, s.twTable,
		yardNode, quayNode,
		startTime, agvID, priority,
		s.MaxWaitTime,
	)
	if route == nil {
		return nil
	}

	basicRoute := router.ConvertTWRouteToRouteResult(route)
	frames := router.DecomposePathWithWaits(basicRoute, route, agvID)

	return &DispatchResult{
		Success:       true,
		Message:       "dispatched after rerouting conflicting AGV",
		Route:         route,
		Frames:        frames,
		RouteID:       fmt.Sprintf("tw-route-%s-%d", taskID, time.Now().UnixMilli()),
		TotalTime:     route.TotalTime,
		TotalDist:     route.TotalDistance,
		Rerouted:      true,
		RerouteVictim: victim,
	}
}

func (s *Scheduler) findAvoidanceRoute(source, target string, startTime float64, agvID string, priority int32) *TWRouteResult {
	originalMaxWait := s.MaxWaitTime
	savedRoutes := make(map[string]bool)

	g := s.graph
	allNodes := g.AllNodes()
	for _, n := range allNodes {
		if n.Type == graph.NodeIntersection {
			if s.twTable.TotalReservationsForNode(n.ID) > 3 {
				savedRoutes[n.ID] = true
				s.twTable.BlockNodeTemporarily(n.ID, agvID)
			}
		}
	}

	route := TimeWindowDijkstra(g, s.twTable, source, target, startTime, agvID, priority, originalMaxWait*2)

	for nodeID := range savedRoutes {
		s.twTable.UnblockNode(nodeID)
	}

	return route
}

func (s *Scheduler) isSafeReservation(reservations []Reservation) bool {
	for _, r := range reservations {
		if !IsSafeState(s.twTable, r) {
			return false
		}
	}
	return true
}

func (s *Scheduler) rerouteAGV(agvID string) bool {
	taskID, ok := s.agvTasks[agvID]
	if !ok {
		return false
	}

	task, ok := s.tasks[taskID]
	if !ok {
		return false
	}

	if task.RerouteCount >= s.RerouteLimit {
		log.Printf("[Scheduler] AGV %s hit reroute limit (%d), releasing reservations", agvID, s.RerouteLimit)
		s.twTable.ReleaseAllByAGV(agvID)
		task.State = TaskStateFailed
		return false
	}

	s.twTable.ReleaseAllByAGV(agvID)

	now := float64(time.Now().UnixMilli()) / 1000.0

	newRoute := TimeWindowDijkstra(
		s.graph, s.twTable,
		task.YardNode, task.QuaysideNode,
		now, agvID, task.Priority,
		s.MaxWaitTime,
	)

	if newRoute == nil {
		log.Printf("[Scheduler] No reroute available for AGV %s", agvID)
		task.State = TaskStateFailed
		return false
	}

	if !s.twTable.ReserveBatch(newRoute.Reservations) {
		log.Printf("[Scheduler] Reroute reservation failed for AGV %s", agvID)
		task.State = TaskStateFailed
		return false
	}

	basicRoute := router.ConvertTWRouteToRouteResult(newRoute)
	frames := router.DecomposePathWithWaits(basicRoute, newRoute, agvID)

	task.Route = newRoute
	task.Frames = frames
	task.State = TaskStateRerouting
	task.RerouteCount++
	task.UpdatedAt = time.Now()
	s.rerouteCount.Add(1)

	if s.mqttClient != nil && s.mqttClient.IsConnected() {
		if err := s.mqttClient.PublishControlFrames(agvID, frames); err != nil {
			log.Printf("[Scheduler] Reroute MQTT publish failed for AGV %s: %v", agvID, err)
		}
	}

	log.Printf("[Scheduler] AGV %s rerouted successfully (attempt %d)", agvID, task.RerouteCount)
	return true
}

func (s *Scheduler) CompleteTask(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return
	}

	s.twTable.ReleaseAllByAGV(task.AgvID)
	task.State = TaskStateCompleted
	task.UpdatedAt = time.Now()
	delete(s.agvTasks, task.AgvID)
	s.tracker.Remove(task.AgvID)

	log.Printf("[Scheduler] Task %s completed, AGV %s reservations released", taskID, task.AgvID)
}

func (s *Scheduler) UpdateAGVPosition(pos *AGVPosition) {
	s.tracker.Update(pos)
}

func (s *Scheduler) RunDeadlockDetection() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	wfg := BuildWaitForGraph(s.twTable)
	cycle := wfg.DetectCycle()
	if cycle == nil {
		return nil
	}

	log.Printf("[Scheduler] Deadlock detected: %v", cycle.AGVIDs)

	selector := NewVictimSelector(s.priorities)
	victim := selector.SelectVictim(cycle)

	if victim != "" {
		s.rerouteAGV(victim)
	}

	return cycle.AGVIDs
}

func (s *Scheduler) StartDeadlockMonitor(interval time.Duration) chan struct{} {
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RunDeadlockDetection()
			case <-stopCh:
				return
			}
		}
	}()
	return stopCh
}

func (s *Scheduler) GetTask(taskID string) (*ScheduledTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	return t, ok
}

func (s *Scheduler) TaskCount() int64 {
	return s.taskCount.Load()
}

func (s *Scheduler) RerouteCount() int64 {
	return s.rerouteCount.Load()
}

func (s *Scheduler) TimeWindowTable() *TimeWindowTable {
	return s.twTable
}

func (s *Scheduler) AGVTracker() *AGVTracker {
	return s.tracker
}
