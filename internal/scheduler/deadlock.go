package scheduler

import (
	"sort"
)

type color int

const (
	white color = iota
	gray
	black
)

type DeadlockCycle struct {
	AGVIDs []string
}

type WaitForGraph struct {
	edges map[string]map[string]bool
}

func NewWaitForGraph() *WaitForGraph {
	return &WaitForGraph{
		edges: make(map[string]map[string]bool),
	}
}

func (wfg *WaitForGraph) AddEdge(waiter, holder string) {
	if _, ok := wfg.edges[waiter]; !ok {
		wfg.edges[waiter] = make(map[string]bool)
	}
	wfg.edges[waiter][holder] = true
}

func (wfg *WaitForGraph) RemoveEdge(waiter, holder string) {
	if neighbors, ok := wfg.edges[waiter]; ok {
		delete(neighbors, holder)
		if len(neighbors) == 0 {
			delete(wfg.edges, waiter)
		}
	}
}

func (wfg *WaitForGraph) RemoveAGV(agvID string) {
	delete(wfg.edges, agvID)
	for _, neighbors := range wfg.edges {
		delete(neighbors, agvID)
	}
}

func (wfg *WaitForGraph) HasEdge(waiter, holder string) bool {
	if neighbors, ok := wfg.edges[waiter]; ok {
		return neighbors[holder]
	}
	return false
}

func (wfg *WaitForGraph) DetectCycle() *DeadlockCycle {
	colors := make(map[string]color)
	parent := make(map[string]string)

	for agvID := range wfg.edges {
		colors[agvID] = white
	}

	for agvID := range wfg.edges {
		if colors[agvID] == white {
			if cycle := wfg.dfs(agvID, colors, parent); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

func (wfg *WaitForGraph) dfs(node string, colors map[string]color, parent map[string]string) *DeadlockCycle {
	colors[node] = gray

	neighbors, ok := wfg.edges[node]
	if ok {
		for neighbor := range neighbors {
			if colors[neighbor] == gray {
				return wfg.extractCycle(node, neighbor, parent)
			}
			if colors[neighbor] == white {
				parent[neighbor] = node
				if cycle := wfg.dfs(neighbor, colors, parent); cycle != nil {
					return cycle
				}
			}
		}
	}

	colors[node] = black
	return nil
}

func (wfg *WaitForGraph) extractCycle(start, end string, parent map[string]string) *DeadlockCycle {
	cycle := []string{end}
	cur := start
	for cur != end {
		cycle = append(cycle, cur)
		cur = parent[cur]
	}
	cycle = append(cycle, end)

	for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
		cycle[i], cycle[j] = cycle[j], cycle[i]
	}

	return &DeadlockCycle{AGVIDs: cycle}
}

func (wfg *WaitForGraph) DetectAllCycles() []DeadlockCycle {
	var cycles []DeadlockCycle
	visited := make(map[string]bool)

	for startNode := range wfg.edges {
		if visited[startNode] {
			continue
		}
		stack := []struct {
			node string
			path []string
		}{{node: startNode, path: []string{startNode}}}

		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			neighbors, ok := wfg.edges[cur.node]
			if !ok {
				continue
			}
			for neighbor := range neighbors {
				for i, n := range cur.path {
					if n == neighbor {
						cycle := make([]string, len(cur.path[i:]))
						copy(cycle, cur.path[i:])
						cycles = append(cycles, DeadlockCycle{AGVIDs: cycle})
						break
					}
				}
				if !visited[neighbor] {
					newPath := make([]string, len(cur.path)+1)
					copy(newPath, cur.path)
					newPath[len(cur.path)] = neighbor
					stack = append(stack, struct {
						node string
						path []string
					}{node: neighbor, path: newPath})
				}
			}
		}
		visited[startNode] = true
	}
	return cycles
}

type VictimSelector struct {
	priorities map[string]int32
}

func NewVictimSelector(priorities map[string]int32) *VictimSelector {
	return &VictimSelector{priorities: priorities}
}

func (vs *VictimSelector) SelectVictim(cycle *DeadlockCycle) string {
	if len(cycle.AGVIDs) == 0 {
		return ""
	}

	type candidate struct {
		agvID    string
		priority int32
	}

	candidates := make([]candidate, 0, len(cycle.AGVIDs)-1)
	for _, agvID := range cycle.AGVIDs[:len(cycle.AGVIDs)-1] {
		prio := int32(0)
		if p, ok := vs.priorities[agvID]; ok {
			prio = p
		}
		candidates = append(candidates, candidate{agvID: agvID, priority: prio})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].priority < candidates[j].priority
	})

	return candidates[0].agvID
}

func BuildWaitForGraph(twt *TimeWindowTable) *WaitForGraph {
	wfg := NewWaitForGraph()
	edges := twt.BuildWaitForEdges()
	for waiter, holders := range edges {
		for holder := range holders {
			wfg.AddEdge(waiter, holder)
		}
	}
	return wfg
}

func IsSafeState(twt *TimeWindowTable, newReservation Reservation) bool {
	twt.Reserve(newReservation)
	wfg := BuildWaitForGraph(twt)
	cycle := wfg.DetectCycle()
	twt.Release(newReservation.AGVID, newReservation.NodeID,
		newReservation.EnterTime, newReservation.ExitTime)
	return cycle == nil
}
