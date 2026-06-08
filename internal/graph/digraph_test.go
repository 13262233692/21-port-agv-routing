package graph

import (
	"math"
	"testing"
)

func buildTestGraph() *Digraph {
	g := NewDigraph()
	g.AddNode(&Node{ID: "A", X: 0, Y: 0})
	g.AddNode(&Node{ID: "B", X: 10, Y: 0})
	g.AddNode(&Node{ID: "C", X: 20, Y: 0})
	g.AddNode(&Node{ID: "D", X: 10, Y: 10})
	g.AddNode(&Node{ID: "E", X: 20, Y: 10})

	g.AddEdge(&Edge{From: "A", To: "B", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&Edge{From: "B", To: "C", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})
	g.AddEdge(&Edge{From: "A", To: "D", BaseWeight: 2.0, Length: 14, Direction: 45, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&Edge{From: "D", To: "E", BaseWeight: 1.5, Length: 10, Direction: 0, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&Edge{From: "B", To: "D", BaseWeight: 1.0, Length: 10, Direction: 90, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&Edge{From: "C", To: "E", BaseWeight: 1.0, Length: 10, Direction: 90, MaxSpeed: 4, IsActive: true})
	g.AddEdge(&Edge{From: "E", To: "C", BaseWeight: 1.0, Length: 10, Direction: 270, MaxSpeed: 4, IsActive: true})

	return g
}

func TestAddNode(t *testing.T) {
	g := NewDigraph()
	g.AddNode(&Node{ID: "A", Type: NodeYard, X: 1, Y: 2})
	if g.NodeCount() != 1 {
		t.Fatalf("expected 1 node, got %d", g.NodeCount())
	}
	n, ok := g.GetNode("A")
	if !ok {
		t.Fatal("node A not found")
	}
	if n.Type != NodeYard {
		t.Fatalf("expected NodeYard, got %d", n.Type)
	}
}

func TestRemoveNode(t *testing.T) {
	g := buildTestGraph()
	g.RemoveNode("B")
	if _, ok := g.GetNode("B"); ok {
		t.Fatal("node B should be removed")
	}
	if g.NodeCount() != 4 {
		t.Fatalf("expected 4 nodes, got %d", g.NodeCount())
	}
}

func TestAddEdge(t *testing.T) {
	g := buildTestGraph()
	if g.EdgeCount() != 7 {
		t.Fatalf("expected 7 edges, got %d", g.EdgeCount())
	}
	e, ok := g.GetEdge("A", "B")
	if !ok {
		t.Fatal("edge A->B not found")
	}
	if e.BaseWeight != 1.0 {
		t.Fatalf("expected base weight 1.0, got %.2f", e.BaseWeight)
	}
}

func TestRemoveEdge(t *testing.T) {
	g := buildTestGraph()
	g.RemoveEdge("A", "B")
	if _, ok := g.GetEdge("A", "B"); ok {
		t.Fatal("edge A->B should be removed")
	}
}

func TestWeightNoCongestion(t *testing.T) {
	g := buildTestGraph()
	w := g.Weight("A", "B")
	if w != 1.0 {
		t.Fatalf("expected weight 1.0, got %.2f", w)
	}
}

func TestWeightWithCongestion(t *testing.T) {
	g := buildTestGraph()
	g.UpdateCongestion("A", "B", 0.5)
	w := g.Weight("A", "B")
	expected := 1.0 * (1.0 + 0.5*3.0)
	if math.Abs(w-expected) > 0.001 {
		t.Fatalf("expected weight %.2f, got %.2f", expected, w)
	}
}

func TestWeightBlockedEdge(t *testing.T) {
	g := buildTestGraph()
	g.BlockEdge("A", "B")
	w := g.Weight("A", "B")
	if !math.IsInf(w, 1) {
		t.Fatalf("expected +Inf for blocked edge, got %.2f", w)
	}
}

func TestUnblockEdge(t *testing.T) {
	g := buildTestGraph()
	g.BlockEdge("A", "B")
	g.UnblockEdge("A", "B")
	w := g.Weight("A", "B")
	if w != 1.0 {
		t.Fatalf("expected weight 1.0 after unblock, got %.2f", w)
	}
}

func TestInactiveEdge(t *testing.T) {
	g := buildTestGraph()
	g.SetEdgeActive("A", "B", false)
	w := g.Weight("A", "B")
	if !math.IsInf(w, 1) {
		t.Fatalf("expected +Inf for inactive edge, got %.2f", w)
	}
}

func TestDeviceStatusBlocking(t *testing.T) {
	g := buildTestGraph()
	g.AddNode(&Node{ID: "F", Type: NodeQuayside, X: 30, Y: 0, DeviceID: "QC-01"})
	g.AddEdge(&Edge{From: "C", To: "F", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})

	g.UpdateDeviceStatus(&DeviceStatus{DeviceID: "QC-01", IsOnline: false, IsOccupied: false, LoadFactor: 0})
	g.ApplyDeviceImpact("QC-01")

	w := g.Weight("C", "F")
	if !math.IsInf(w, 1) {
		t.Fatalf("expected +Inf for offline device edge, got %.2f", w)
	}

	g.UpdateDeviceStatus(&DeviceStatus{DeviceID: "QC-01", IsOnline: true, IsOccupied: false, LoadFactor: 0})
	g.ApplyDeviceImpact("QC-01")

	w = g.Weight("C", "F")
	if w != 1.0 {
		t.Fatalf("expected weight 1.0 after device back online, got %.2f", w)
	}
}

func TestDeviceOccupiedIncreasesCongestion(t *testing.T) {
	g := buildTestGraph()
	g.AddNode(&Node{ID: "F", Type: NodeYard, X: 30, Y: 0, DeviceID: "YC-01"})
	g.AddEdge(&Edge{From: "A", To: "F", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6, IsActive: true})

	g.UpdateDeviceStatus(&DeviceStatus{DeviceID: "YC-01", IsOnline: true, IsOccupied: true, LoadFactor: 0.5})
	g.ApplyDeviceImpact("YC-01")

	w := g.Weight("A", "F")
	if w <= 1.0 {
		t.Fatalf("expected increased weight for occupied device, got %.2f", w)
	}
}

func TestGetNeighbors(t *testing.T) {
	g := buildTestGraph()
	neighbors := g.GetNeighbors("A")
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 neighbors for A, got %d", len(neighbors))
	}
}

func TestGetOutEdges(t *testing.T) {
	g := buildTestGraph()
	edges := g.GetOutEdges("A")
	if len(edges) != 2 {
		t.Fatalf("expected 2 out-edges for A, got %d", len(edges))
	}
}

func TestNonExistentEdgeWeight(t *testing.T) {
	g := buildTestGraph()
	w := g.Weight("Z", "A")
	if !math.IsInf(w, 1) {
		t.Fatalf("expected +Inf for non-existent edge, got %.2f", w)
	}
}
