package graph

import (
	"encoding/json"
	"os"
)

type NodeJSON struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	DeviceID string  `json:"device_id,omitempty"`
}

type EdgeJSON struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	BaseWeight float64 `json:"base_weight"`
	MaxSpeed   float64 `json:"max_speed"`
	Length     float64 `json:"length"`
	Direction  float64 `json:"direction"`
}

type TopologyJSON struct {
	Nodes []NodeJSON `json:"nodes"`
	Edges []EdgeJSON `json:"edges"`
}

func nodeTypeFromString(s string) NodeType {
	switch s {
	case "yard":
		return NodeYard
	case "quayside":
		return NodeQuayside
	case "charging":
		return NodeChargingStation
	case "buffer":
		return NodeBufferZone
	default:
		return NodeIntersection
	}
}

func LoadTopology(filePath string) (*Digraph, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var topo TopologyJSON
	if err := json.Unmarshal(data, &topo); err != nil {
		return nil, err
	}
	g := NewDigraph()
	for _, n := range topo.Nodes {
		g.AddNode(&Node{
			ID:       n.ID,
			Type:     nodeTypeFromString(n.Type),
			X:        n.X,
			Y:        n.Y,
			DeviceID: n.DeviceID,
		})
	}
	for _, e := range topo.Edges {
		g.AddEdge(&Edge{
			From:       e.From,
			To:         e.To,
			BaseWeight: e.BaseWeight,
			Congestion: 0,
			IsBlocked:  false,
			MaxSpeed:   e.MaxSpeed,
			Length:     e.Length,
			Direction:  e.Direction,
			IsActive:   true,
		})
	}
	return g, nil
}
