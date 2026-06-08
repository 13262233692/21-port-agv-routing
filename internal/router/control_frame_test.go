package router

import (
	"testing"

	"github.com/port-agv/routing/internal/graph"
)

func TestDecomposePathNil(t *testing.T) {
	frames := DecomposePath(nil, "AGV-01")
	if frames != nil {
		t.Fatal("expected nil for nil route")
	}
}

func TestDecomposePathSingle(t *testing.T) {
	route := &RouteResult{
		Path: []PathNode{{ID: "A", X: 0, Y: 0}},
	}
	frames := DecomposePath(route, "AGV-01")
	if frames != nil {
		t.Fatal("expected nil for single-node path")
	}
}

func TestDecomposePathStraight(t *testing.T) {
	route := &RouteResult{
		Path: []PathNode{
			{ID: "A", X: 0, Y: 0, Angle: 0},
			{ID: "B", X: 10, Y: 0, Angle: 0},
		},
		Edges: []*graph.Edge{
			{From: "A", To: "B", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6},
		},
	}
	frames := DecomposePath(route, "AGV-01")
	if len(frames) < 3 {
		t.Fatalf("expected at least 3 frames (start+straight+decel+stop), got %d", len(frames))
	}
	if frames[0].Maneuver != ManeuverStart {
		t.Fatalf("expected first frame to be Start, got %d", frames[0].Maneuver)
	}
	lastFrame := frames[len(frames)-1]
	if lastFrame.Maneuver != ManeuverStop {
		t.Fatalf("expected last frame to be Stop, got %d", lastFrame.Maneuver)
	}
	for _, f := range frames {
		if f.AgvID != "AGV-01" {
			t.Fatalf("expected AgvID AGV-01, got %s", f.AgvID)
		}
	}
}

func TestDecomposePathWithTurn(t *testing.T) {
	route := &RouteResult{
		Path: []PathNode{
			{ID: "A", X: 0, Y: 0, Angle: 0},
			{ID: "B", X: 10, Y: 0, Angle: 0},
			{ID: "C", X: 10, Y: 10, Angle: 90},
		},
		Edges: []*graph.Edge{
			{From: "A", To: "B", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6},
			{From: "B", To: "C", BaseWeight: 1.0, Length: 10, Direction: 90, MaxSpeed: 4},
		},
	}
	frames := DecomposePath(route, "AGV-01")

	hasTurn := false
	for _, f := range frames {
		if f.Maneuver == ManeuverTurnRight {
			hasTurn = true
			break
		}
	}
	if !hasTurn {
		t.Fatal("expected a turn-right maneuver in frames for 90-degree right turn")
	}
}

func TestDecomposePathWithUTurn(t *testing.T) {
	route := &RouteResult{
		Path: []PathNode{
			{ID: "A", X: 0, Y: 0, Angle: 0},
			{ID: "B", X: 10, Y: 0, Angle: 180},
		},
		Edges: []*graph.Edge{
			{From: "A", To: "B", BaseWeight: 1.0, Length: 10, Direction: 180, MaxSpeed: 3},
		},
	}
	frames := DecomposePath(route, "AGV-02")

	hasUTurn := false
	for _, f := range frames {
		if f.Maneuver == ManeuverUTurn {
			hasUTurn = true
			break
		}
	}
	if !hasUTurn {
		t.Fatal("expected a U-turn maneuver for 180-degree direction change")
	}
}

func TestDecomposePathSequenceOrdered(t *testing.T) {
	route := &RouteResult{
		Path: []PathNode{
			{ID: "A", X: 0, Y: 0, Angle: 0},
			{ID: "B", X: 10, Y: 0, Angle: 0},
			{ID: "C", X: 20, Y: 0, Angle: 0},
		},
		Edges: []*graph.Edge{
			{From: "A", To: "B", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6},
			{From: "B", To: "C", BaseWeight: 1.0, Length: 10, Direction: 0, MaxSpeed: 6},
		},
	}
	frames := DecomposePath(route, "AGV-01")
	for i, f := range frames {
		if f.Sequence != i {
			t.Fatalf("frame %d has sequence %d, expected %d", i, f.Sequence, i)
		}
	}
}
