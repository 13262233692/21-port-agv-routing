package router

import (
	"container/heap"
	"math"

	"github.com/port-agv/routing/internal/graph"
)

type PathNode struct {
	ID    string
	X     float64
	Y     float64
	Angle float64
}

type RouteResult struct {
	Path     []PathNode
	Distance float64
	Weight   float64
	Edges    []*graph.Edge
}

type pqItem struct {
	nodeID   string
	distance float64
	index    int
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].distance < pq[j].distance
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*pqItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

func Dijkstra(g *graph.Digraph, source, target string) *RouteResult {
	dist := make(map[string]float64)
	prev := make(map[string]string)
	prevEdge := make(map[string]*graph.Edge)
	visited := make(map[string]bool)

	pq := make(priorityQueue, 0)
	heap.Init(&pq)

	allNodes := g.AllNodes()
	for _, n := range allNodes {
		dist[n.ID] = math.Inf(1)
	}
	dist[source] = 0
	heap.Push(&pq, &pqItem{nodeID: source, distance: 0})

	for pq.Len() > 0 {
		current := heap.Pop(&pq).(*pqItem)
		curID := current.nodeID

		if visited[curID] {
			continue
		}
		visited[curID] = true

		if curID == target {
			break
		}

		neighbors := g.GetNeighbors(curID)
		for _, neighbor := range neighbors {
			if visited[neighbor] {
				continue
			}
			w := g.Weight(curID, neighbor)
			alt := dist[curID] + w
			if alt < dist[neighbor] {
				dist[neighbor] = alt
				prev[neighbor] = curID
				if e, ok := g.GetEdge(curID, neighbor); ok {
					prevEdge[neighbor] = e
				}
				heap.Push(&pq, &pqItem{nodeID: neighbor, distance: alt})
			}
		}
	}

	if dist[target] == math.Inf(1) {
		return nil
	}

	path := make([]PathNode, 0)
	edgeList := make([]*graph.Edge, 0)
	totalDist := 0.0
	cur := target
	for cur != "" {
		node, ok := g.GetNode(cur)
		if !ok {
			break
		}
		pn := PathNode{
			ID: node.ID,
			X:  node.X,
			Y:  node.Y,
		}
		if e, ok := prevEdge[cur]; ok {
			pn.Angle = e.Direction
			totalDist += e.Length
			edgeList = append(edgeList, e)
		}
		path = append(path, pn)
		cur = prev[cur]
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	for i, j := 0, len(edgeList)-1; i < j; i, j = i+1, j-1 {
		edgeList[i], edgeList[j] = edgeList[j], edgeList[i]
	}

	return &RouteResult{
		Path:     path,
		Distance: totalDist,
		Weight:   dist[target],
		Edges:    edgeList,
	}
}

func DijkstraMultiTarget(g *graph.Digraph, source string, targets []string) *RouteResult {
	dist := make(map[string]float64)
	prev := make(map[string]string)
	prevEdge := make(map[string]*graph.Edge)
	visited := make(map[string]bool)
	targetSet := make(map[string]bool)
	for _, t := range targets {
		targetSet[t] = true
	}

	pq := make(priorityQueue, 0)
	heap.Init(&pq)

	allNodes := g.AllNodes()
	for _, n := range allNodes {
		dist[n.ID] = math.Inf(1)
	}
	dist[source] = 0
	heap.Push(&pq, &pqItem{nodeID: source, distance: 0})

	bestTarget := ""
	bestDist := math.Inf(1)
	foundCount := 0

	for pq.Len() > 0 {
		current := heap.Pop(&pq).(*pqItem)
		curID := current.nodeID

		if visited[curID] {
			continue
		}
		visited[curID] = true

		if targetSet[curID] {
			if dist[curID] < bestDist {
				bestDist = dist[curID]
				bestTarget = curID
			}
			foundCount++
			if foundCount >= len(targetSet) {
				break
			}
		}

		neighbors := g.GetNeighbors(curID)
		for _, neighbor := range neighbors {
			if visited[neighbor] {
				continue
			}
			w := g.Weight(curID, neighbor)
			alt := dist[curID] + w
			if alt < dist[neighbor] {
				dist[neighbor] = alt
				prev[neighbor] = curID
				if e, ok := g.GetEdge(curID, neighbor); ok {
					prevEdge[neighbor] = e
				}
				heap.Push(&pq, &pqItem{nodeID: neighbor, distance: alt})
			}
		}
	}

	if bestTarget == "" {
		return nil
	}

	path := make([]PathNode, 0)
	edgeList := make([]*graph.Edge, 0)
	totalDist := 0.0
	cur := bestTarget
	for cur != "" {
		node, ok := g.GetNode(cur)
		if !ok {
			break
		}
		pn := PathNode{
			ID: node.ID,
			X:  node.X,
			Y:  node.Y,
		}
		if e, ok := prevEdge[cur]; ok {
			pn.Angle = e.Direction
			totalDist += e.Length
			edgeList = append(edgeList, e)
		}
		path = append(path, pn)
		cur = prev[cur]
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	for i, j := 0, len(edgeList)-1; i < j; i, j = i+1, j-1 {
		edgeList[i], edgeList[j] = edgeList[j], edgeList[i]
	}

	return &RouteResult{
		Path:     path,
		Distance: totalDist,
		Weight:   bestDist,
		Edges:    edgeList,
	}
}

func ConvertTWRouteToRouteResult(twRoute interface {
	GetPathNodes() []PathNode
	GetEdges() []*graph.Edge
	GetTotalDistance() float64
	GetTotalTime() float64
}) *RouteResult {
	if twRoute == nil {
		return nil
	}
	pathNodes := twRoute.GetPathNodes()
	path := make([]PathNode, len(pathNodes))
	copy(path, pathNodes)
	return &RouteResult{
		Path:     path,
		Distance: twRoute.GetTotalDistance(),
		Weight:   twRoute.GetTotalTime(),
		Edges:    twRoute.GetEdges(),
	}
}
