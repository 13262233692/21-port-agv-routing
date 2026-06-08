package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/port-agv/routing/api/proto/agv"
	"github.com/port-agv/routing/internal/graph"
	"github.com/port-agv/routing/internal/mqtt"
	"github.com/port-agv/routing/internal/router"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskDispatched
	TaskInProgress
	TaskCompleted
	TaskFailed
)

type TaskInfo struct {
	TaskID       string
	ContainerID  string
	AgvID        string
	YardNode     string
	QuaysideNode string
	Priority     int32
	Deadline     int64
	Status       TaskStatus
	Route        *router.RouteResult
	Frames       []router.ControlFrame
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Server struct {
	pb.UnimplementedAGVDispatchServiceServer

	graph      *graph.Digraph
	mqttClient *mqtt.Client

	mu        sync.RWMutex
	tasks     map[string]*TaskInfo
	taskCount atomic.Int64

	grpcServer *grpc.Server
	port       int
}

func NewServer(g *graph.Digraph, mqttClient *mqtt.Client, port int) *Server {
	return &Server{
		graph:      g,
		mqttClient: mqttClient,
		tasks:      make(map[string]*TaskInfo),
		port:       port,
	}
}

func (s *Server) DispatchTask(ctx context.Context, req *pb.DispatchTaskRequest) (*pb.DispatchTaskResponse, error) {
	log.Printf("[gRPC] DispatchTask: task=%s container=%s agv=%s yard=%s quay=%s",
		req.TaskId, req.ContainerId, req.AgvId, req.YardNodeId, req.QuaysideNodeId)

	route := router.Dijkstra(s.graph, req.YardNodeId, req.QuaysideNodeId)
	if route == nil {
		return &pb.DispatchTaskResponse{
			Success: false,
			Message: fmt.Sprintf("no route found from %s to %s", req.YardNodeId, req.QuaysideNodeId),
		}, nil
	}

	frames := router.DecomposePath(route, req.AgvId)

	task := &TaskInfo{
		TaskID:       req.TaskId,
		ContainerID:  req.ContainerId,
		AgvID:        req.AgvId,
		YardNode:     req.YardNodeId,
		QuaysideNode: req.QuaysideNodeId,
		Priority:     req.Priority,
		Deadline:     req.DeadlineUnix,
		Status:       TaskDispatched,
		Route:        route,
		Frames:       frames,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	s.mu.Lock()
	s.tasks[req.TaskId] = task
	s.mu.Unlock()
	s.taskCount.Add(1)

	if s.mqttClient != nil && s.mqttClient.IsConnected() {
		if err := s.mqttClient.PublishControlFrames(req.AgvId, frames); err != nil {
			log.Printf("[gRPC] MQTT publish failed for AGV %s: %v", req.AgvId, err)
			task.Status = TaskFailed
			return &pb.DispatchTaskResponse{
				Success: false,
				Message: fmt.Sprintf("MQTT publish failed: %v", err),
			}, nil
		}
		task.Status = TaskInProgress
	}

	pbFrames := make([]*pb.ControlFrameProto, len(frames))
	for i, f := range frames {
		pbFrames[i] = &pb.ControlFrameProto{
			Sequence:    int32(f.Sequence),
			NodeId:      f.NodeID,
			Maneuver:    int32(f.Maneuver),
			Speed:       f.Speed,
			TargetAngle: f.TargetAngle,
			DeltaAngle:  f.DeltaAngle,
			Distance:    f.Distance,
			AgvId:       f.AgvID,
		}
	}

	estimatedTime := 0.0
	if route.Distance > 0 {
		estimatedTime = route.Distance / 3.0
	}

	return &pb.DispatchTaskResponse{
		Success:        true,
		Message:        "task dispatched successfully",
		RouteId:        fmt.Sprintf("route-%s-%d", req.TaskId, time.Now().UnixMilli()),
		Frames:         pbFrames,
		TotalDistance:  route.Distance,
		EstimatedTime:  estimatedTime,
	}, nil
}

func (s *Server) StreamTaskStatus(req *pb.StreamTaskStatusRequest, stream pb.AGVDispatchService_StreamTaskStatusServer) error {
	log.Printf("[gRPC] StreamTaskStatus: task=%s", req.TaskId)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			s.mu.RLock()
			task, ok := s.tasks[req.TaskId]
			s.mu.RUnlock()

			if !ok {
				return fmt.Errorf("task %s not found", req.TaskId)
			}

			update := &pb.TaskStatusUpdate{
				TaskId:        task.TaskID,
				AgvId:         task.AgvID,
				Status:        int32(task.Status),
				CurrentNodeId: task.YardNode,
				Progress:      0.0,
				Timestamp:     time.Now().UnixMilli(),
			}

			if task.Status == TaskInProgress {
				elapsed := time.Since(task.UpdatedAt).Seconds()
				if task.Route != nil && task.Route.Distance > 0 {
					estTime := task.Route.Distance / 3.0
					if estTime > 0 {
						update.Progress = min(elapsed/estTime, 1.0)
					}
				}
			}

			if err := stream.Send(update); err != nil {
				return err
			}

			if task.Status == TaskCompleted || task.Status == TaskFailed {
				return nil
			}
		}
	}
}

func (s *Server) GetRoute(ctx context.Context, req *pb.GetRouteRequest) (*pb.GetRouteResponse, error) {
	log.Printf("[gRPC] GetRoute: source=%s target=%s", req.SourceNodeId, req.TargetNodeId)

	var route *router.RouteResult
	if len(req.IntermediateNodeIds) > 0 {
		targets := append(req.IntermediateNodeIds, req.TargetNodeId)
		route = router.DijkstraMultiTarget(s.graph, req.SourceNodeId, targets)
	} else {
		route = router.Dijkstra(s.graph, req.SourceNodeId, req.TargetNodeId)
	}

	if route == nil {
		return &pb.GetRouteResponse{Found: false}, nil
	}

	pbPath := make([]*pb.PathNodeProto, len(route.Path))
	for i, pn := range route.Path {
		pbPath[i] = &pb.PathNodeProto{
			Id:     pn.ID,
			X:      pn.X,
			Y:      pn.Y,
			Angle:  pn.Angle,
		}
	}

	pbEdges := make([]*pb.EdgeInfoProto, len(route.Edges))
	for i, e := range route.Edges {
		pbEdges[i] = &pb.EdgeInfoProto{
			From:       e.From,
			To:         e.To,
			Weight:     s.graph.Weight(e.From, e.To),
			Length:     e.Length,
			Congestion: e.Congestion,
		}
	}

	return &pb.GetRouteResponse{
		Found:         true,
		Path:          pbPath,
		TotalDistance: route.Distance,
		TotalWeight:   route.Weight,
		Edges:         pbEdges,
	}, nil
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}

	s.grpcServer = grpc.NewServer(
		grpc.MaxConcurrentStreams(1000),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  30 * time.Second,
			Timeout:               10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	pb.RegisterAGVDispatchServiceServer(s.grpcServer, s)
	reflection.Register(s.grpcServer)

	log.Printf("[gRPC] Server starting on port %d", s.port)
	if err := s.grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("grpc serve failed: %w", err)
	}
	return nil
}

func (s *Server) Stop() {
	if s.grpcServer != nil {
		log.Println("[gRPC] Server stopping...")
		s.grpcServer.GracefulStop()
	}
}

func (s *Server) TaskCount() int64 {
	return s.taskCount.Load()
}

func (s *Server) GetTask(taskID string) (*TaskInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskID]
	return t, ok
}
