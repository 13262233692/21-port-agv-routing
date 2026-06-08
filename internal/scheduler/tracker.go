package scheduler

import (
	"sync"
	"time"
)

type AGVPosition struct {
	AGVID        string
	CurrentNode  string
	NextNode     string
	Speed        float64
	Heading      float64
	Battery      float64
	Status       string
	ETA          float64
	LastUpdate   time.Time
	RouteID      string
}

type AGVTracker struct {
	mu        sync.RWMutex
	positions map[string]*AGVPosition
}

func NewAGVTracker() *AGVTracker {
	return &AGVTracker{
		positions: make(map[string]*AGVPosition),
	}
}

func (at *AGVTracker) Update(pos *AGVPosition) {
	at.mu.Lock()
	defer at.mu.Unlock()
	pos.LastUpdate = time.Now()
	at.positions[pos.AGVID] = pos
}

func (at *AGVTracker) Get(agvID string) (*AGVPosition, bool) {
	at.mu.RLock()
	defer at.mu.RUnlock()
	p, ok := at.positions[agvID]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

func (at *AGVTracker) CurrentNode(agvID string) string {
	at.mu.RLock()
	defer at.mu.RUnlock()
	if p, ok := at.positions[agvID]; ok {
		return p.CurrentNode
	}
	return ""
}

func (at *AGVTracker) AllActive() []*AGVPosition {
	at.mu.RLock()
	defer at.mu.RUnlock()
	result := make([]*AGVPosition, 0, len(at.positions))
	for _, p := range at.positions {
		cp := *p
		result = append(result, &cp)
	}
	return result
}

func (at *AGVTracker) ActiveCount() int {
	at.mu.RLock()
	defer at.mu.RUnlock()
	return len(at.positions)
}

func (at *AGVTracker) Remove(agvID string) {
	at.mu.Lock()
	defer at.mu.Unlock()
	delete(at.positions, agvID)
}
