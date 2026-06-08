package router

import (
	"math"
	"testing"

	"github.com/port-agv/routing/internal/graph"
)

func buildRouterTestGraph() *graph.Digraph {
	g := graph.NewDigraph()
	g.AddNode(&graph.Node{ID: "S", X: 0, Y: 0, Type: graph.NodeYard})
	g.AddNode(&graph.Node{ID: "A", X: 10, Y: 0, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "B", X: 0, Y: 10, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "C", X: 10, Y: 10, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "T", X: 20, Y: 10, Type: graph.NodeQuayside})

	g.AddEdge(&graph.Edge{From: "S", To: "A", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "S", To: "B", BaseWeight: 4.0, Length: 10, Direction: 90, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "A", To: "C", BaseWeight: 2.0, Length: 10, Direction: 90, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "B", To: "C", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&graph.Edge{From: "C", To: "T", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})

	return g
}

func TestDijkstraBasic(t *testing.T) {
	g := buildRouterTestGraph()
	result := Dijkstra(g, "S", "T")
	if result == nil {
		t.Fatal("expected route, got nil")
	}
	if len(result.Path) < 2 {
		t.Fatalf("expected at least 2 nodes in path, got %d", len(result.Path))
	}
	if result.Path[0].ID != "S" {
		t.Fatalf("expected path to start at S, got %s", result.Path[0].ID)
	}
	if result.Path[len(result.Path)-1].ID != "T" {
		t.Fatalf("expected path to end at T, got %s", result.Path[len(result.Path)-1].ID)
	}
	if math.Abs(result.Weight-4.0) > 0.001 {
		t.Fatalf("expected weight 4.0, got %.2f", result.Weight)
	}
}

func TestDijkstraSameSourceTarget(t *testing.T) {
	g := buildRouterTestGraph()
	result := Dijkstra(g, "S", "S")
	if result == nil {
		t.Fatal("expected route for same source/target")
	}
	if result.Weight != 0 {
		t.Fatalf("expected weight 0 for same source/target, got %.2f", result.Weight)
	}
}

func TestDijkstraNoPath(t *testing.T) {
	g := buildRouterTestGraph()
	g.AddNode(&graph.Node{ID: "X", X: 100, Y: 100, Type: graph.NodeIntersection})
	result := Dijkstra(g, "S", "X")
	if result != nil {
		t.Fatal("expected nil for unreachable target")
	}
}

func TestDijkstraWithCongestion(t *testing.T) {
	g := buildRouterTestGraph()
	result1 := Dijkstra(g, "S", "T")
	if result1 == nil {
		t.Fatal("expected route before congestion")
	}
	initialWeight := result1.Weight

	g.UpdateCongestion("A", "C", 1.0)
	result2 := Dijkstra(g, "S", "T")
	if result2 == nil {
		t.Fatal("expected route after congestion")
	}

	if result2.Weight <= initialWeight {
		t.Fatalf("expected increased weight after congestion, initial=%.2f after=%.2f", initialWeight, result2.Weight)
	}
}

func TestDijkstraBlockedEdge(t *testing.T) {
	g := buildRouterTestGraph()
	g.BlockEdge("A", "C")
	result := Dijkstra(g, "S", "T")
	if result == nil {
		t.Fatal("expected alternate route after blocking")
	}
	for _, p := range result.Path {
		if p.ID == "A" {
			t.Fatal("path should not go through A when A->C is blocked and S->B->C is cheaper")
		}
	}
}

func TestDijkstraMultiTarget(t *testing.T) {
	g := buildRouterTestGraph()
	g.AddNode(&graph.Node{ID: "T2", X: 20, Y: 0, Type: graph.NodeQuayside})
	g.AddEdge(&graph.Edge{From: "A", To: "T2", BaseWeight: 0.5, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})

	result := DijkstraMultiTarget(g, "S", []string{"T", "T2"})
	if result == nil {
		t.Fatal("expected route to one of the targets")
	}
	if result.Path[0].ID != "S" {
		t.Fatalf("expected path to start at S, got %s", result.Path[0].ID)
	}
	if result.Weight > 2.0 {
		t.Fatalf("expected weight <= 2.0 for nearest target, got %.2f", result.Weight)
	}
}

func TestDijkstraMultiTargetNoReachable(t *testing.T) {
	g := buildRouterTestGraph()
	g.AddNode(&graph.Node{ID: "X", X: 100, Y: 100, Type: graph.NodeIntersection})
	g.AddNode(&graph.Node{ID: "Y", X: 200, Y: 200, Type: graph.NodeIntersection})

	result := DijkstraMultiTarget(g, "S", []string{"X", "Y"})
	if result != nil {
		t.Fatal("expected nil for unreachable targets")
	}
}
