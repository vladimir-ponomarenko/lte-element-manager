package pm

import "sync"

type Store struct {
	mu     sync.RWMutex
	latest Report
	ok     bool
}

func NewStore() *Store { return &Store{} }

func (s *Store) Update(r Report) {
	s.mu.Lock()
	s.latest = r
	s.ok = true
	s.mu.Unlock()
}

func (s *Store) Latest() (Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest, s.ok
}
