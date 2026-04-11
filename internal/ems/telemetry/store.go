package telemetry

import (
	"sync"

	"lte-element-manager/internal/ems/domain/canonical"
)

type Store struct {
	mu     sync.RWMutex
	latest []canonical.Sample
}

func NewStore() *Store { return &Store{} }

func (s *Store) Update(samples []canonical.Sample) {
	s.mu.Lock()
	s.latest = samples
	s.mu.Unlock()
}

func (s *Store) Latest() []canonical.Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]canonical.Sample, len(s.latest))
	copy(out, s.latest)
	return out
}
