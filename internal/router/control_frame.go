package router

import (
	"math"

	"github.com/port-agv/routing/internal/graph"
)

type ManeuverType int

const (
	ManeuverStraight ManeuverType = iota
	ManeuverTurnLeft
	ManeuverTurnRight
	ManeuverUTurn
	ManeuverAccelerate
	ManeuverDecelerate
	ManeuverStop
	ManeuverStart
	ManeuverWait
)

type ControlFrame struct {
	Sequence     int         `json:"seq"`
	NodeID       string      `json:"node_id"`
	Maneuver     ManeuverType `json:"maneuver"`
	Speed        float64     `json:"speed"`
	TargetAngle  float64     `json:"target_angle"`
	DeltaAngle   float64     `json:"delta_angle"`
	Distance     float64     `json:"distance"`
	WaitDuration float64     `json:"wait_duration,omitempty"`
	AgvID        string      `json:"agv_id,omitempty"`
}

const (
	TurnThresholdSmall  = 15.0
	TurnThresholdMedium = 45.0
	TurnThresholdLarge  = 135.0
	MinSpeed            = 0.5
	MaxSpeed            = 6.0
	TurnSpeed           = 1.5
	DecelDistance       = 5.0
	AccelDistance       = 3.0
)

func normalizeAngle(angle float64) float64 {
	for angle > 180 {
		angle -= 360
	}
	for angle <= -180 {
		angle += 360
	}
	return angle
}

func angleDelta(from, to float64) float64 {
	return normalizeAngle(to - from)
}

func classifyTurn(delta float64) ManeuverType {
	abs := math.Abs(delta)
	switch {
	case abs < TurnThresholdSmall:
		return ManeuverStraight
	case abs < TurnThresholdMedium:
		if delta > 0 {
			return ManeuverTurnRight
		}
		return ManeuverTurnLeft
	case abs < TurnThresholdLarge:
		if delta > 0 {
			return ManeuverTurnRight
		}
		return ManeuverTurnLeft
	default:
		return ManeuverUTurn
	}
}

func DecomposePath(route *RouteResult, agvID string) []ControlFrame {
	if route == nil || len(route.Path) < 2 {
		return nil
	}

	var frames []ControlFrame
	seq := 0

	frames = append(frames, ControlFrame{
		Sequence: seq,
		NodeID:   route.Path[0].ID,
		Maneuver: ManeuverStart,
		Speed:    MinSpeed,
	})
	seq++

	for i := 1; i < len(route.Path); i++ {
		prev := route.Path[i-1]
		curr := route.Path[i]

		var edge *graph.Edge
		if i-1 < len(route.Edges) {
			edge = route.Edges[i-1]
		}

		delta := angleDelta(prev.Angle, curr.Angle)
		maneuver := classifyTurn(delta)

		speed := MaxSpeed
		if edge != nil && edge.MaxSpeed < MaxSpeed {
			speed = edge.MaxSpeed
		}
		if maneuver != ManeuverStraight {
			speed = TurnSpeed
		}

		distance := 0.0
		if edge != nil {
			distance = edge.Length
		}

		if distance > AccelDistance && i == 1 {
			frames = append(frames, ControlFrame{
				Sequence:    seq,
				NodeID:      prev.ID,
				Maneuver:    ManeuverAccelerate,
				Speed:       speed,
				TargetAngle: curr.Angle,
				DeltaAngle:  delta,
				Distance:    AccelDistance,
			})
			seq++
		}

		frames = append(frames, ControlFrame{
			Sequence:    seq,
			NodeID:      curr.ID,
			Maneuver:    maneuver,
			Speed:       speed,
			TargetAngle: curr.Angle,
			DeltaAngle:  delta,
			Distance:    distance,
		})
		seq++

		if i == len(route.Path)-1 {
			frames = append(frames, ControlFrame{
				Sequence:    seq,
				NodeID:      curr.ID,
				Maneuver:    ManeuverDecelerate,
				Speed:       MinSpeed,
				TargetAngle: curr.Angle,
				Distance:    DecelDistance,
			})
			seq++
		}
	}

	frames = append(frames, ControlFrame{
		Sequence: seq,
		NodeID:   route.Path[len(route.Path)-1].ID,
		Maneuver: ManeuverStop,
		Speed:    0,
	})

	for i := range frames {
		frames[i].AgvID = agvID
	}

	return frames
}

func DecomposePathWithWaits(route *RouteResult, twRoute interface{ GetPathWaitTimes() []float64 }, agvID string) []ControlFrame {
	if route == nil || len(route.Path) < 2 {
		return nil
	}

	frames := DecomposePath(route, agvID)
	if frames == nil {
		return nil
	}

	waitTimes := twRoute.GetPathWaitTimes()
	if len(waitTimes) != len(route.Path) {
		return frames
	}

	var newFrames []ControlFrame
	seq := 0

	for _, f := range frames {
		pathIdx := -1
		for pi, pn := range route.Path {
			if pn.ID == f.NodeID {
				pathIdx = pi
				break
			}
		}

		if pathIdx >= 0 && pathIdx < len(waitTimes) && waitTimes[pathIdx] > 0.1 {
			switch f.Maneuver {
			case ManeuverStart, ManeuverStraight, ManeuverTurnLeft, ManeuverTurnRight, ManeuverUTurn:
				newFrames = append(newFrames, ControlFrame{
					Sequence:     seq,
					NodeID:       f.NodeID,
					Maneuver:     f.Maneuver,
					Speed:        f.Speed,
					TargetAngle:  f.TargetAngle,
					DeltaAngle:   f.DeltaAngle,
					Distance:     f.Distance,
					WaitDuration: 0,
				})
				seq++

				newFrames = append(newFrames, ControlFrame{
					Sequence:     seq,
					NodeID:       f.NodeID,
					Maneuver:     ManeuverDecelerate,
					Speed:        MinSpeed,
					TargetAngle:  f.TargetAngle,
					WaitDuration: 0,
				})
				seq++

				newFrames = append(newFrames, ControlFrame{
					Sequence:     seq,
					NodeID:       f.NodeID,
					Maneuver:     ManeuverWait,
					Speed:        0,
					WaitDuration: waitTimes[pathIdx],
				})
				seq++

				newFrames = append(newFrames, ControlFrame{
					Sequence:     seq,
					NodeID:       f.NodeID,
					Maneuver:     ManeuverStart,
					Speed:        MinSpeed,
					WaitDuration: 0,
				})
				seq++

			default:
				f.Sequence = seq
				newFrames = append(newFrames, f)
				seq++
			}
		} else {
			f.Sequence = seq
			newFrames = append(newFrames, f)
			seq++
		}
	}

	for i := range newFrames {
		newFrames[i].AgvID = agvID
	}

	return newFrames
}
