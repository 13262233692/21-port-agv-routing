package scheduler

import (
	"math"
	"sort"
	"sync"
)

const DefaultSafetyMargin = 2.0

type Reservation struct {
	AGVID     string
	NodeID    string
	EnterTime float64
	ExitTime  float64
	Priority  int32
}

type NodeTimeline struct {
	mu           sync.RWMutex
	NodeID       string
	Reservations []Reservation
	SafetyMargin float64
}

func NewNodeTimeline(nodeID string) *NodeTimeline {
	return &NodeTimeline{
		NodeID:       nodeID,
		Reservations: make([]Reservation, 0),
		SafetyMargin: DefaultSafetyMargin,
	}
}

func (nt *NodeTimeline) overlaps(enter, exit float64, excludeAGV string) *Reservation {
	for i := range nt.Reservations {
		r := &nt.Reservations[i]
		if r.AGVID == excludeAGV {
			continue
		}
		rEnter := r.EnterTime - nt.SafetyMargin
		rExit := r.ExitTime + nt.SafetyMargin
		if enter < rExit && exit > rEnter {
			return r
		}
	}
	return nil
}

func (nt *NodeTimeline) IsAvailable(enter, exit float64, excludeAGV string) bool {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	return nt.overlaps(enter, exit, excludeAGV) == nil
}

func (nt *NodeTimeline) NextAvailableTime(earliestEnter, duration float64, excludeAGV string) float64 {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	enter := earliestEnter
	exit := enter + duration

	for {
		conflict := nt.overlaps(enter, exit, excludeAGV)
		if conflict == nil {
			return enter
		}
		enter = conflict.ExitTime + nt.SafetyMargin
		exit = enter + duration
	}
}

func (nt *NodeTimeline) Reserve(reservation Reservation) bool {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if nt.overlaps(reservation.EnterTime, reservation.ExitTime, reservation.AGVID) != nil {
		return false
	}

	idx := sort.Search(len(nt.Reservations), func(i int) bool {
		return nt.Reservations[i].EnterTime > reservation.EnterTime
	})

	nt.Reservations = append(nt.Reservations, Reservation{})
	copy(nt.Reservations[idx+1:], nt.Reservations[idx:])
	nt.Reservations[idx] = reservation

	return true
}

func (nt *NodeTimeline) ReleaseByAGV(agvID string) int {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	n := 0
	filtered := make([]Reservation, 0, len(nt.Reservations))
	for _, r := range nt.Reservations {
		if r.AGVID == agvID {
			n++
		} else {
			filtered = append(filtered, r)
		}
	}
	nt.Reservations = filtered
	return n
}

func (nt *NodeTimeline) GetReservationsByAGV(agvID string) []Reservation {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	var result []Reservation
	for _, r := range nt.Reservations {
		if r.AGVID == agvID {
			result = append(result, r)
		}
	}
	return result
}

func (nt *NodeTimeline) ConflictingAGVs(enter, exit float64, excludeAGV string) []string {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	seen := make(map[string]bool)
	var result []string
	for _, r := range nt.Reservations {
		if r.AGVID == excludeAGV || seen[r.AGVID] {
			continue
		}
		rEnter := r.EnterTime - nt.SafetyMargin
		rExit := r.ExitTime + nt.SafetyMargin
		if enter < rExit && exit > rEnter {
			seen[r.AGVID] = true
			result = append(result, r.AGVID)
		}
	}
	return result
}

func (nt *NodeTimeline) ReservationCount() int {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	return len(nt.Reservations)
}

type TimeWindowTable struct {
	mu      sync.RWMutex
	nodes   map[string]*NodeTimeline
	agvIdx  map[string]map[string]bool
}

func NewTimeWindowTable() *TimeWindowTable {
	return &TimeWindowTable{
		nodes:  make(map[string]*NodeTimeline),
		agvIdx: make(map[string]map[string]bool),
	}
}

func (twt *TimeWindowTable) EnsureNode(nodeID string) {
	twt.mu.Lock()
	defer twt.mu.Unlock()
	if _, ok := twt.nodes[nodeID]; !ok {
		twt.nodes[nodeID] = NewNodeTimeline(nodeID)
	}
}

func (twt *TimeWindowTable) Reserve(reservation Reservation) bool {
	twt.EnsureNode(reservation.NodeID)

	twt.mu.RLock()
	timeline := twt.nodes[reservation.NodeID]
	twt.mu.RUnlock()

	if !timeline.Reserve(reservation) {
		return false
	}

	twt.mu.Lock()
	if _, ok := twt.agvIdx[reservation.AGVID]; !ok {
		twt.agvIdx[reservation.AGVID] = make(map[string]bool)
	}
	twt.agvIdx[reservation.AGVID][reservation.NodeID] = true
	twt.mu.Unlock()

	return true
}

func (twt *TimeWindowTable) ReserveBatch(reservations []Reservation) bool {
	for i := range reservations {
		if !twt.Reserve(reservations[i]) {
			for j := 0; j < i; j++ {
				twt.Release(reservations[j].AGVID, reservations[j].NodeID,
					reservations[j].EnterTime, reservations[j].ExitTime)
			}
			return false
		}
	}
	return true
}

func (twt *TimeWindowTable) Release(agvID, nodeID string, enterTime, exitTime float64) bool {
	twt.mu.RLock()
	timeline, ok := twt.nodes[nodeID]
	twt.mu.RUnlock()
	if !ok {
		return false
	}

	timeline.mu.Lock()
	defer timeline.mu.Unlock()

	for i, r := range timeline.Reservations {
		if r.AGVID == agvID && math.Abs(r.EnterTime-enterTime) < 0.001 && math.Abs(r.ExitTime-exitTime) < 0.001 {
			timeline.Reservations = append(timeline.Reservations[:i], timeline.Reservations[i+1:]...)
			return true
		}
	}
	return false
}

func (twt *TimeWindowTable) ReleaseAllByAGV(agvID string) int {
	twt.mu.RLock()
	nodeSet, ok := twt.agvIdx[agvID]
	if !ok {
		twt.mu.RUnlock()
		return 0
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	twt.mu.RUnlock()

	total := 0
	for _, nodeID := range nodes {
		twt.mu.RLock()
		timeline, ok := twt.nodes[nodeID]
		twt.mu.RUnlock()
		if ok {
			total += timeline.ReleaseByAGV(agvID)
		}
	}

	twt.mu.Lock()
	delete(twt.agvIdx, agvID)
	twt.mu.Unlock()

	return total
}

func (twt *TimeWindowTable) IsAvailable(nodeID string, enter, exit float64, excludeAGV string) bool {
	twt.mu.RLock()
	timeline, ok := twt.nodes[nodeID]
	twt.mu.RUnlock()
	if !ok {
		return true
	}
	return timeline.IsAvailable(enter, exit, excludeAGV)
}

func (twt *TimeWindowTable) NextAvailableTime(nodeID string, earliestEnter, duration float64, excludeAGV string) float64 {
	twt.mu.RLock()
	timeline, ok := twt.nodes[nodeID]
	twt.mu.RUnlock()
	if !ok {
		return earliestEnter
	}
	return timeline.NextAvailableTime(earliestEnter, duration, excludeAGV)
}

func (twt *TimeWindowTable) ConflictingAGVs(nodeID string, enter, exit float64, excludeAGV string) []string {
	twt.mu.RLock()
	timeline, ok := twt.nodes[nodeID]
	twt.mu.RUnlock()
	if !ok {
		return nil
	}
	return timeline.ConflictingAGVs(enter, exit, excludeAGV)
}

func (twt *TimeWindowTable) GetAGVReservations(agvID string) []Reservation {
	twt.mu.RLock()
	nodeSet, ok := twt.agvIdx[agvID]
	if !ok {
		twt.mu.RUnlock()
		return nil
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	twt.mu.RUnlock()

	var result []Reservation
	for _, nodeID := range nodes {
		twt.mu.RLock()
		timeline, ok := twt.nodes[nodeID]
		twt.mu.RUnlock()
		if ok {
			result = append(result, timeline.GetReservationsByAGV(agvID)...)
		}
	}
	return result
}

func (twt *TimeWindowTable) TotalReservations() int {
	twt.mu.RLock()
	defer twt.mu.RUnlock()
	total := 0
	for _, timeline := range twt.nodes {
		total += timeline.ReservationCount()
	}
	return total
}

func (twt *TimeWindowTable) BuildWaitForEdges() map[string]map[string]bool {
	twt.mu.RLock()
	defer twt.mu.RUnlock()

	edges := make(map[string]map[string]bool)
	for _, timeline := range twt.nodes {
		timeline.mu.RLock()
		for i, r1 := range timeline.Reservations {
			for j, r2 := range timeline.Reservations {
				if i == j || r1.AGVID == r2.AGVID {
					continue
				}
				r1Enter := r1.EnterTime - timeline.SafetyMargin
				r1Exit := r1.ExitTime + timeline.SafetyMargin
				r2Enter := r2.EnterTime - timeline.SafetyMargin
				r2Exit := r2.ExitTime + timeline.SafetyMargin
				if r1Enter < r2Exit && r1Exit > r2Enter {
					if r1.EnterTime >= r2.EnterTime {
						if _, ok := edges[r1.AGVID]; !ok {
							edges[r1.AGVID] = make(map[string]bool)
						}
						edges[r1.AGVID][r2.AGVID] = true
					}
				}
			}
		}
		timeline.mu.RUnlock()
	}
	return edges
}

type blockedNode struct {
	NodeID    string
	BlockedBy string
}

var blockedNodes sync.Map

func (twt *TimeWindowTable) BlockNodeTemporarily(nodeID, excludeAGV string) {
	blockedNodes.Store(nodeID, blockedNode{NodeID: nodeID, BlockedBy: excludeAGV})
}

func (twt *TimeWindowTable) UnblockNode(nodeID string) {
	blockedNodes.Delete(nodeID)
}

func (twt *TimeWindowTable) TotalReservationsForNode(nodeID string) int {
	twt.mu.RLock()
	timeline, ok := twt.nodes[nodeID]
	twt.mu.RUnlock()
	if !ok {
		return 0
	}
	return timeline.ReservationCount()
}
