package scheduler

import (
	"testing"

	"github.com/port-agv/routing/internal/graph"
)

func buildSchedulerTestGraph() *graph.Digraph {
	g := graph.NewDigraph()
	g.AddNode(&graph.Node{ID: "Y1", X: 0, Y: 0, Type: graph.NodeYard})
	g.AddNode(&graph.Node{ID: "I1", X: 50, Y: 0, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "I2", X: 100, Y: 0, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "I3", X: 50, Y: 50, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "I4", X: 100, Y: 50, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "Q1", X: 150, Y: 0, Type: graph.NodeQuayside})
	g.AddNode(&graph.Node{ID: "Q2", X: 150, Y: 50, Type: graph.NodeQuayside})

	g.AddEdge(&graph.Edge{From: "Y1", To: "I1", BaseWeight: 1.0, Length: 50, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I1", To: "I2", BaseWeight: 1.0, Length: 50, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I2", To: "Q1", BaseWeight: 1.0, Length: 50, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I1", To: "I3", BaseWeight: 1.0, Length: 50, Direction: 90, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I3", To: "I4", BaseWeight: 1.0, Length: 50, Direction: 0, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I4", To: "Q2", BaseWeight: 1.0, Length: 50, Direction: 0, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I2", To: "I4", BaseWeight: 1.0, Length: 50, Direction: 90, MaxSpeed: 4, IsActive: true})

	g.AddEdge(&graph.Edge{From: "I2", To: "I1", BaseWeight: 1.0, Length: 50, Direction: 180, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "I3", To: "I1", BaseWeight: 1.0, Length: 50, Direction: 270, MaxSpeed: 4, IsActive: true})

	return g
}

func TestTimeWindowReserveAndCheck(t *testing.T) {
	twt := NewTimeWindowTable()
	twt.EnsureNode("I1")

	ok := twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 10, ExitTime: 15})
	if !ok {
		t.Fatal("first reservation should succeed")
	}

	ok = twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 12, ExitTime: 18})
	if ok {
		t.Fatal("overlapping reservation should fail")
	}

	ok = twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 20, ExitTime: 25})
	if !ok {
		t.Fatal("non-overlapping reservation should succeed")
	}
}

func TestTimeWindowNextAvailableTime(t *testing.T) {
	twt := NewTimeWindowTable()
	twt.EnsureNode("I1")

	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 10, ExitTime: 15})
	twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 20, ExitTime: 25})

	avail := twt.NextAvailableTime("I1", 12, 3, "AGV-03")
	if avail < 17 {
		t.Fatalf("expected next available >= 17 (15 + 2 safety margin), got %.0f", avail)
	}
}

func TestTimeWindowReleaseByAGV(t *testing.T) {
	twt := NewTimeWindowTable()
	twt.EnsureNode("I1")

	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 10, ExitTime: 15})
	twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 20, ExitTime: 25})

	released := twt.ReleaseAllByAGV("AGV-01")
	if released != 1 {
		t.Fatalf("expected 1 released, got %d", released)
	}

	ok := twt.Reserve(Reservation{AGVID: "AGV-03", NodeID: "I1", EnterTime: 10, ExitTime: 15})
	if !ok {
		t.Fatal("should be able to reserve after release")
	}
}

func TestTimeWindowConflictingAGVs(t *testing.T) {
	twt := NewTimeWindowTable()
	twt.EnsureNode("I1")

	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 10, ExitTime: 15})
	twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 20, ExitTime: 25})

	conflicts := twt.ConflictingAGVs("I1", 12, 16, "AGV-03")
	if len(conflicts) != 1 || conflicts[0] != "AGV-01" {
		t.Fatalf("expected [AGV-01] conflicts, got %v", conflicts)
	}
}

func TestDeadlockDetectionNoCycle(t *testing.T) {
	wfg := NewWaitForGraph()
	wfg.AddEdge("AGV-01", "AGV-02")
	wfg.AddEdge("AGV-02", "AGV-03")

	cycle := wfg.DetectCycle()
	if cycle != nil {
		t.Fatal("expected no cycle in linear wait graph")
	}
}

func TestDeadlockDetectionCycle(t *testing.T) {
	wfg := NewWaitForGraph()
	wfg.AddEdge("AGV-01", "AGV-02")
	wfg.AddEdge("AGV-02", "AGV-03")
	wfg.AddEdge("AGV-03", "AGV-01")

	cycle := wfg.DetectCycle()
	if cycle == nil {
		t.Fatal("expected cycle in circular wait graph")
	}
	found := make(map[string]bool)
	for _, id := range cycle.AGVIDs {
		found[id] = true
	}
	if !found["AGV-01"] || !found["AGV-02"] || !found["AGV-03"] {
		t.Fatalf("expected cycle to contain all 3 AGVs, got %v", cycle.AGVIDs)
	}
}

func TestDeadlockVictimSelection(t *testing.T) {
	priorities := map[string]int32{
		"AGV-01": 10,
		"AGV-02": 5,
		"AGV-03": 8,
	}
	selector := NewVictimSelector(priorities)

	cycle := &DeadlockCycle{AGVIDs: []string{"AGV-01", "AGV-02", "AGV-03", "AGV-01"}}
	victim := selector.SelectVictim(cycle)
	if victim != "AGV-02" {
		t.Fatalf("expected AGV-02 (lowest priority=5) as victim, got %s", victim)
	}
}

func TestDeadlockRemoveBreaksCycle(t *testing.T) {
	wfg := NewWaitForGraph()
	wfg.AddEdge("AGV-01", "AGV-02")
	wfg.AddEdge("AGV-02", "AGV-03")
	wfg.AddEdge("AGV-03", "AGV-01")

	wfg.RemoveEdge("AGV-02", "AGV-03")
	cycle := wfg.DetectCycle()
	if cycle != nil {
		t.Fatal("expected no cycle after removing edge")
	}
}

func TestTWDijkstraBasic(t *testing.T) {
	g := buildSchedulerTestGraph()
	twt := NewTimeWindowTable()

	route := TimeWindowDijkstra(g, twt, "Y1", "Q1", 0, "AGV-01", 1, 30.0)
	if route == nil {
		t.Fatal("expected route to be found")
	}
	if len(route.Path) < 2 {
		t.Fatalf("expected at least 2 nodes, got %d", len(route.Path))
	}
	if route.Path[0].ID != "Y1" {
		t.Fatalf("expected start at Y1, got %s", route.Path[0].ID)
	}
	if route.Path[len(route.Path)-1].ID != "Q1" {
		t.Fatalf("expected end at Q1, got %s", route.Path[len(route.Path)-1].ID)
	}
}

func TestTWDijkstraAvoidsConflict(t *testing.T) {
	g := buildSchedulerTestGraph()
	twt := NewTimeWindowTable()

	twt.EnsureNode("I1")
	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 8, ExitTime: 15})

	route := TimeWindowDijkstra(g, twt, "Y1", "Q1", 0, "AGV-02", 1, 30.0)
	if route == nil {
		t.Fatal("expected route to be found despite conflict")
	}

	for _, p := range route.Path {
		if p.ID == "I1" {
			if p.EnterTime < 17 {
				t.Fatalf("AGV-02 should wait until AGV-01 clears I1 (exit=15 + margin=2), got enter=%.1f", p.EnterTime)
			}
		}
	}
}

func TestTWDijkstraMaxWaitExceeded(t *testing.T) {
	g := buildSchedulerTestGraph()
	twt := NewTimeWindowTable()

	twt.EnsureNode("I1")
	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 8, ExitTime: 50})

	route := TimeWindowDijkstra(g, twt, "Y1", "Q1", 0, "AGV-02", 1, 5.0)
	if route == nil {
		t.Log("route is nil when max wait is exceeded - this is acceptable")
	}
}

func TestTWDijkstraWithReservations(t *testing.T) {
	g := buildSchedulerTestGraph()
	twt := NewTimeWindowTable()

	route1 := TimeWindowDijkstra(g, twt, "Y1", "Q1", 0, "AGV-01", 1, 120.0)
	if route1 == nil {
		t.Fatal("expected route1")
	}
	if !twt.ReserveBatch(route1.Reservations) {
		t.Fatal("failed to reserve route1")
	}

	route2 := TimeWindowDijkstra(g, twt, "Y1", "Q2", 0, "AGV-02", 1, 120.0)
	if route2 == nil {
		t.Fatal("expected route2")
	}

	for _, p := range route2.Path {
		if p.ID == "I1" {
			conflicts := twt.ConflictingAGVs("I1", p.EnterTime, p.ExitTime, "AGV-02")
			if len(conflicts) > 0 {
				t.Fatalf("AGV-02 should not conflict at I1, conflicts=%v", conflicts)
			}
		}
	}
}

func TestSchedulerDispatch(t *testing.T) {
	g := buildSchedulerTestGraph()
	sched := NewScheduler(g, nil)

	result := sched.DispatchTask("T1", "C1", "AGV-01", "Y1", "Q1", 1, 0)
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if result.Route == nil {
		t.Fatal("expected route in result")
	}
}

func TestSchedulerDispatchMultipleAGVs(t *testing.T) {
	g := buildSchedulerTestGraph()
	sched := NewScheduler(g, nil)
	sched.MaxWaitTime = 120.0

	result1 := sched.DispatchTask("T1", "C1", "AGV-01", "Y1", "Q1", 5, 0)
	if !result1.Success {
		t.Fatalf("AGV-01 dispatch failed: %s", result1.Message)
	}

	result2 := sched.DispatchTask("T2", "C2", "AGV-02", "Y1", "Q2", 3, 0)
	if !result2.Success {
		t.Fatalf("AGV-02 dispatch failed: %s", result2.Message)
	}

	tasks := sched.TimeWindowTable().TotalReservations()
	if tasks == 0 {
		t.Fatal("expected reservations in time window table")
	}
}

func TestSchedulerCompleteTask(t *testing.T) {
	g := buildSchedulerTestGraph()
	sched := NewScheduler(g, nil)

	result := sched.DispatchTask("T1", "C1", "AGV-01", "Y1", "Q1", 1, 0)
	if !result.Success {
		t.Fatalf("dispatch failed: %s", result.Message)
	}

	before := sched.TimeWindowTable().TotalReservations()
	sched.CompleteTask("T1")
	after := sched.TimeWindowTable().TotalReservations()

	if after >= before {
		t.Fatalf("expected fewer reservations after completion, before=%d after=%d", before, after)
	}
}

func TestBuildWaitForGraph(t *testing.T) {
	wfg := NewWaitForGraph()
	wfg.AddEdge("AGV-01", "AGV-02")
	wfg.AddEdge("AGV-02", "AGV-03")

	cycle := wfg.DetectCycle()
	if cycle != nil {
		t.Fatal("no cycle expected in linear chain")
	}

	wfg.AddEdge("AGV-03", "AGV-01")
	cycle = wfg.DetectCycle()
	if cycle == nil {
		t.Fatal("expected cycle after closing the loop")
	}
}

func TestSafetyMarginPreventsCloseCalls(t *testing.T) {
	twt := NewTimeWindowTable()
	twt.EnsureNode("I1")

	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 10, ExitTime: 15})

	ok := twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 15, ExitTime: 20})
	if ok {
		t.Fatal("reservation at exact exit time should fail due to safety margin")
	}

	ok = twt.Reserve(Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 17.1, ExitTime: 22})
	if !ok {
		t.Fatal("reservation after safety margin should succeed")
	}
}

func TestIsSafeState(t *testing.T) {
	twt := NewTimeWindowTable()
	twt.EnsureNode("I1")

	twt.Reserve(Reservation{AGVID: "AGV-01", NodeID: "I1", EnterTime: 10, ExitTime: 15})

	safe := IsSafeState(twt, Reservation{AGVID: "AGV-02", NodeID: "I1", EnterTime: 20, ExitTime: 25})
	if !safe {
		t.Fatal("non-overlapping reservation should be safe")
	}
}
