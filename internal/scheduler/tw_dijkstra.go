package scheduler

import (
	"container/heap"
	"math"

	"github.com/port-agv/routing/internal/graph"
	"github.com/port-agv/routing/internal/router"
)

const (
	CrossingTimeIntersection  = 5.0
	CrossingTimeYard          = 30.0
	CrossingTimeQuayside      = 30.0
	CrossingTimeBuffer        = 3.0
	CrossingTimeCharging      = 60.0
	CrossingTimeDefault       = 5.0
	DefaultMaxWaitTime        = 30.0
)

func getCrossingTime(nodeType graph.NodeType) float64 {
	switch nodeType {
	case graph.NodeIntersection:
		return CrossingTimeIntersection
	case graph.NodeYard:
		return CrossingTimeYard
	case graph.NodeQuayside:
		return CrossingTimeQuayside
	case graph.NodeBufferZone:
		return CrossingTimeBuffer
	case graph.NodeChargingStation:
		return CrossingTimeCharging
	default:
		return CrossingTimeDefault
	}
}

type TWPathNode struct {
	ID         string
	X          float64
	Y          float64
	Angle      float64
	EnterTime  float64
	ExitTime   float64
	WaitTime   float64
}

type TWRouteResult struct {
	Path         []TWPathNode
	Edges        []*graph.Edge
	TotalTime    float64
	TotalDistance float64
	Reservations []Reservation
}

func (r *TWRouteResult) GetPathWaitTimes() []float64 {
	if r == nil {
		return nil
	}
	wt := make([]float64, len(r.Path))
	for i, p := range r.Path {
		wt[i] = p.WaitTime
	}
	return wt
}

func (r *TWRouteResult) GetPathNodes() []router.PathNode {
	if r == nil {
		return nil
	}
	nodes := make([]router.PathNode, len(r.Path))
	for i, p := range r.Path {
		nodes[i] = router.PathNode{
			ID:    p.ID,
			X:     p.X,
			Y:     p.Y,
			Angle: p.Angle,
		}
	}
	return nodes
}

func (r *TWRouteResult) GetEdges() []*graph.Edge {
	if r == nil {
		return nil
	}
	return r.Edges
}

func (r *TWRouteResult) GetTotalDistance() float64 {
	if r == nil {
		return 0
	}
	return r.TotalDistance
}

func (r *TWRouteResult) GetTotalTime() float64 {
	if r == nil {
		return 0
	}
	return r.TotalTime
}

type twPQItem struct {
	nodeID      string
	arrivalTime float64
	index       int
}

type twPriorityQueue []*twPQItem

func (pq twPriorityQueue) Len() int { return len(pq) }

func (pq twPriorityQueue) Less(i, j int) bool {
	return pq[i].arrivalTime < pq[j].arrivalTime
}

func (pq twPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *twPriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*twPQItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *twPriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

type TWRouteStep struct {
	NodeID     string
	ArriveTime float64
	ExitTime   float64
	WaitTime   float64
}

func TimeWindowDijkstra(
	g *graph.Digraph,
	twt *TimeWindowTable,
	source, target string,
	startTime float64,
	agvID string,
	priority int32,
	maxWaitTime float64,
) *TWRouteResult {
	dist := make(map[string]float64)
	prev := make(map[string]string)
	prevEdge := make(map[string]*graph.Edge)
	prevStep := make(map[string]*TWRouteStep)
	visited := make(map[string]bool)

	allNodes := g.AllNodes()
	for _, n := range allNodes {
		dist[n.ID] = math.Inf(1)
	}

	sourceNode, srcOK := g.GetNode(source)
	if !srcOK {
		return nil
	}

	sourceCrossing := getCrossingTime(sourceNode.Type)
	sourceExit := startTime + sourceCrossing

	twt.EnsureNode(source)
	if !twt.IsAvailable(source, startTime, sourceExit, agvID) {
		availAt := twt.NextAvailableTime(source, startTime, sourceCrossing, agvID)
		wait := availAt - startTime
		if wait > maxWaitTime {
			return nil
		}
		startTime = availAt
		sourceExit = startTime + sourceCrossing
	}

	dist[source] = startTime
	prevStep[source] = &TWRouteStep{
		NodeID:     source,
		ArriveTime: startTime,
		ExitTime:   sourceExit,
		WaitTime:   0,
	}

	pq := make(twPriorityQueue, 0)
	heap.Init(&pq)
	heap.Push(&pq, &twPQItem{nodeID: source, arrivalTime: startTime})

	for pq.Len() > 0 {
		current := heap.Pop(&pq).(*twPQItem)
		curID := current.nodeID
		curArrival := current.arrivalTime

		if visited[curID] {
			continue
		}
		visited[curID] = true

		if curID == target {
			break
		}

		var curExit float64
		if step, ok := prevStep[curID]; ok {
			curExit = step.ExitTime
		} else {
			curExit = curArrival
		}

		neighbors := g.GetNeighbors(curID)
		for _, neighbor := range neighbors {
			if visited[neighbor] {
				continue
			}

			edge, ok := g.GetEdge(curID, neighbor)
			if !ok {
				continue
			}

			travelTime := edge.Length / edge.MaxSpeed
			congMu := edge.Congestion
			travelTime *= (1.0 + congMu*3.0)

			estArrival := curExit + travelTime

			neighborNode, nbOK := g.GetNode(neighbor)
			if !nbOK {
				continue
			}

			crossingTime := getCrossingTime(neighborNode.Type)
			estExit := estArrival + crossingTime

			twt.EnsureNode(neighbor)

			var actualEnter float64
			var waitTime float64

			if twt.IsAvailable(neighbor, estArrival, estExit, agvID) {
				actualEnter = estArrival
				waitTime = 0
			} else {
				availAt := twt.NextAvailableTime(neighbor, estArrival, crossingTime, agvID)
				waitTime = availAt - estArrival
				if waitTime > maxWaitTime {
					continue
				}
				actualEnter = availAt
			}

			actualExit := actualEnter + crossingTime
			totalArrival := actualEnter

			if totalArrival < dist[neighbor] {
				dist[neighbor] = totalArrival
				prev[neighbor] = curID
				prevEdge[neighbor] = edge
				prevStep[neighbor] = &TWRouteStep{
					NodeID:     neighbor,
					ArriveTime: actualEnter,
					ExitTime:   actualExit,
					WaitTime:   waitTime,
				}
				heap.Push(&pq, &twPQItem{nodeID: neighbor, arrivalTime: totalArrival})
			}
		}
	}

	if dist[target] == math.Inf(1) {
		return nil
	}

	path := make([]TWPathNode, 0)
	edgeList := make([]*graph.Edge, 0)
	reservations := make([]Reservation, 0)
	totalDist := 0.0

	cur := target
	for cur != "" {
		step, stepOK := prevStep[cur]
		if !stepOK {
			break
		}

		node, nodeOK := g.GetNode(cur)
		if !nodeOK {
			break
		}

		pn := TWPathNode{
			ID:        node.ID,
			X:         node.X,
			Y:         node.Y,
			EnterTime: step.ArriveTime,
			ExitTime:  step.ExitTime,
			WaitTime:  step.WaitTime,
		}

		if e, ok := prevEdge[cur]; ok {
			pn.Angle = e.Direction
			totalDist += e.Length
			edgeList = append(edgeList, e)
		}

		path = append(path, pn)
		reservations = append(reservations, Reservation{
			AGVID:     agvID,
			NodeID:    cur,
			EnterTime: step.ArriveTime,
			ExitTime:  step.ExitTime,
			Priority:  priority,
		})

		cur = prev[cur]
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	for i, j := 0, len(edgeList)-1; i < j; i, j = i+1, j-1 {
		edgeList[i], edgeList[j] = edgeList[j], edgeList[i]
	}
	for i, j := 0, len(reservations)-1; i < j; i, j = i+1, j-1 {
		reservations[i], reservations[j] = reservations[j], reservations[i]
	}

	totalTime := 0.0
	if len(path) > 0 {
		lastStep := prevStep[target]
		if lastStep != nil {
			totalTime = lastStep.ExitTime - startTime
		}
	}

	return &TWRouteResult{
		Path:         path,
		Edges:        edgeList,
		TotalTime:    totalTime,
		TotalDistance: totalDist,
		Reservations: reservations,
	}
}
