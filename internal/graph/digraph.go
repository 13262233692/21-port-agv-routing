package graph

import (
	"math"
	"sync"
)

type NodeType int

const (
	NodeIntersection NodeType = iota
	NodeYard
	NodeQuayside
	NodeChargingStation
	NodeBufferZone
)

type Node struct {
	ID       string
	Type     NodeType
	X        float64
	Y        float64
	DeviceID string
}

type Edge struct {
	From         string
	To           string
	BaseWeight   float64
	Congestion   float64
	IsBlocked    bool
	MaxSpeed     float64
	Length       float64
	Direction    float64
	IsActive     bool
}

type DeviceStatus struct {
	DeviceID   string
	IsOnline   bool
	IsOccupied bool
	LoadFactor float64
}

type Digraph struct {
	mu          sync.RWMutex
	nodes       map[string]*Node
	edges       map[string]map[string]*Edge
	outEdges    map[string][]string
	inEdges     map[string][]string
	devices     map[string]*DeviceStatus
	congestionMu sync.RWMutex
	edgeCongestion map[string]map[string]float64
}

func NewDigraph() *Digraph {
	return &Digraph{
		nodes:          make(map[string]*Node),
		edges:          make(map[string]map[string]*Edge),
		outEdges:       make(map[string][]string),
		inEdges:        make(map[string][]string),
		devices:        make(map[string]*DeviceStatus),
		edgeCongestion: make(map[string]map[string]float64),
	}
}

func (g *Digraph) AddNode(node *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[node.ID] = node
	if _, ok := g.edges[node.ID]; !ok {
		g.edges[node.ID] = make(map[string]*Edge)
	}
	if _, ok := g.edgeCongestion[node.ID]; !ok {
		g.edgeCongestion[node.ID] = make(map[string]float64)
	}
}

func (g *Digraph) RemoveNode(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, to := range g.outEdges[id] {
		delete(g.edges[id], to)
		delete(g.edgeCongestion[id], to)
	}
	for _, from := range g.inEdges[id] {
		delete(g.edges[from], id)
		delete(g.edgeCongestion[from], id)
	}
	delete(g.nodes, id)
	delete(g.edges, id)
	delete(g.outEdges, id)
	delete(g.inEdges, id)
	delete(g.edgeCongestion, id)
}

func (g *Digraph) AddEdge(edge *Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.edges[edge.From]; !ok {
		g.edges[edge.From] = make(map[string]*Edge)
	}
	if _, ok := g.edgeCongestion[edge.From]; !ok {
		g.edgeCongestion[edge.From] = make(map[string]float64)
	}
	g.edges[edge.From][edge.To] = edge
	g.edgeCongestion[edge.From][edge.To] = edge.Congestion
	found := false
	for _, v := range g.outEdges[edge.From] {
		if v == edge.To {
			found = true
			break
		}
	}
	if !found {
		g.outEdges[edge.From] = append(g.outEdges[edge.From], edge.To)
	}
	found = false
	for _, v := range g.inEdges[edge.To] {
		if v == edge.From {
			found = true
			break
		}
	}
	if !found {
		g.inEdges[edge.To] = append(g.inEdges[edge.To], edge.From)
	}
}

func (g *Digraph) RemoveEdge(from, to string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.edges[from], to)
	delete(g.edgeCongestion[from], to)
	newOut := make([]string, 0, len(g.outEdges[from]))
	for _, v := range g.outEdges[from] {
		if v != to {
			newOut = append(newOut, v)
		}
	}
	g.outEdges[from] = newOut
	newIn := make([]string, 0, len(g.inEdges[to]))
	for _, v := range g.inEdges[to] {
		if v != from {
			newIn = append(newIn, v)
		}
	}
	g.inEdges[to] = newIn
}

func (g *Digraph) GetNode(id string) (*Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

func (g *Digraph) GetEdge(from, to string) (*Edge, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	edges, ok := g.edges[from]
	if !ok {
		return nil, false
	}
	e, ok := edges[to]
	return e, ok
}

func (g *Digraph) GetOutEdges(from string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	edges, ok := g.edges[from]
	if !ok {
		return nil
	}
	result := make([]*Edge, 0, len(edges))
	for _, e := range edges {
		result = append(result, e)
	}
	return result
}

func (g *Digraph) GetNeighbors(from string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]string{}, g.outEdges[from]...)
}

func (g *Digraph) Weight(from, to string) float64 {
	g.mu.RLock()
	e, ok := g.edges[from]
	g.mu.RUnlock()
	if !ok {
		return math.Inf(1)
	}
	g.mu.RLock()
	edge, ok2 := e[to]
	g.mu.RUnlock()
	if !ok2 {
		return math.Inf(1)
	}
	if edge.IsBlocked || !edge.IsActive {
		return math.Inf(1)
	}
	g.congestionMu.RLock()
	cong := g.edgeCongestion[from][to]
	g.congestionMu.RUnlock()
	congestionFactor := 1.0 + cong*3.0
	return edge.BaseWeight * congestionFactor
}

func (g *Digraph) UpdateCongestion(from, to string, congestion float64) {
	g.congestionMu.Lock()
	defer g.congestionMu.Unlock()
	if _, ok := g.edgeCongestion[from]; !ok {
		g.edgeCongestion[from] = make(map[string]float64)
	}
	g.edgeCongestion[from][to] = congestion
}

func (g *Digraph) BlockEdge(from, to string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if edges, ok := g.edges[from]; ok {
		if e, ok2 := edges[to]; ok2 {
			e.IsBlocked = true
		}
	}
}

func (g *Digraph) UnblockEdge(from, to string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if edges, ok := g.edges[from]; ok {
		if e, ok2 := edges[to]; ok2 {
			e.IsBlocked = false
		}
	}
}

func (g *Digraph) SetEdgeActive(from, to string, active bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if edges, ok := g.edges[from]; ok {
		if e, ok2 := edges[to]; ok2 {
			e.IsActive = active
		}
	}
}

func (g *Digraph) UpdateDeviceStatus(status *DeviceStatus) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.devices[status.DeviceID] = status
}

func (g *Digraph) GetDeviceStatus(deviceID string) (*DeviceStatus, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.devices[deviceID]
	return s, ok
}

func (g *Digraph) GetInNeighbors(to string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]string{}, g.inEdges[to]...)
}

func (g *Digraph) ApplyDeviceImpact(deviceID string) {
	g.mu.RLock()
	status, ok := g.devices[deviceID]
	if !ok {
		g.mu.RUnlock()
		return
	}
	g.mu.RUnlock()

	g.mu.RLock()
	var affectedNodes []string
	for _, node := range g.nodes {
		if node.DeviceID == deviceID {
			affectedNodes = append(affectedNodes, node.ID)
		}
	}
	g.mu.RUnlock()

	for _, nodeID := range affectedNodes {
		outNeighbors := g.GetNeighbors(nodeID)
		for _, neighbor := range outNeighbors {
			if !status.IsOnline {
				g.BlockEdge(nodeID, neighbor)
			} else {
				g.UnblockEdge(nodeID, neighbor)
			}
			if status.IsOccupied {
				cong := 0.3 + status.LoadFactor*0.5
				g.UpdateCongestion(nodeID, neighbor, cong)
			}
		}

		inNeighbors := g.GetInNeighbors(nodeID)
		for _, neighbor := range inNeighbors {
			if !status.IsOnline {
				g.BlockEdge(neighbor, nodeID)
			} else {
				g.UnblockEdge(neighbor, nodeID)
			}
			if status.IsOccupied {
				cong := 0.3 + status.LoadFactor*0.5
				g.UpdateCongestion(neighbor, nodeID, cong)
			}
		}
	}
}

func (g *Digraph) AllNodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		result = append(result, n)
	}
	return result
}

func (g *Digraph) AllEdges() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []*Edge
	for _, edges := range g.edges {
		for _, e := range edges {
			result = append(result, e)
		}
	}
	return result
}

func (g *Digraph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

func (g *Digraph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, edges := range g.edges {
		count += len(edges)
	}
	return count
}
