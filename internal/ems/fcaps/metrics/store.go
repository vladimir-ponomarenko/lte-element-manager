package metrics

import (
	"sync"

	"lte-element-manager/internal/ems/domain"
)

// Store keeps the latest metric sample in memory for NETCONF/NMS reads.
type Store struct {
	mu     sync.RWMutex
	latest domain.MetricSample
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Update(sample domain.MetricSample) {
	s.mu.Lock()
	s.latest = sample
	s.mu.Unlock()
}

func (s *Store) Latest() domain.MetricSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}
