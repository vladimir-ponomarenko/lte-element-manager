package alarms

import (
	"sync"
	"time"

	"lte-element-manager/internal/ems/domain"
)

// Store keeps active alarms and their history counters in memory.
type Store struct {
	mu      sync.Mutex
	records map[Key]Record
}

func NewStore() *Store {
	return &Store{records: map[Key]Record{}}
}

// Upsert marks alarm as active and updates counters.
func (s *Store) Upsert(at time.Time, component string, alarm domain.Alarm) (Record, bool) {
	k := Key{Component: component, Code: alarm.Code}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.records[k]
	if !ok {
		rec = Record{
			Key:       k,
			Status:    StatusActive,
			Message:   alarm.Message,
			Severity:  alarm.Severity,
			FirstSeen: at,
			LastSeen:  at,
			Count:     1,
		}
		s.records[k] = rec
		return rec, true
	}

	changed := false
	if rec.Status != StatusActive {
		rec.Status = StatusActive
		changed = true
	}
	if rec.Message != alarm.Message {
		rec.Message = alarm.Message
		changed = true
	}
	if rec.Severity != alarm.Severity {
		rec.Severity = alarm.Severity
		changed = true
	}
	rec.LastSeen = at
	rec.Count++

	s.records[k] = rec
	return rec, changed
}

// ClearComponent clears all active alarms for a component and returns the cleared records.
func (s *Store) ClearComponent(at time.Time, component string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cleared []Record
	for k, rec := range s.records {
		if k.Component != component {
			continue
		}
		if rec.Status == StatusCleared {
			continue
		}
		rec.Status = StatusCleared
		rec.LastSeen = at
		s.records[k] = rec
		cleared = append(cleared, rec)
	}
	return cleared
}

func (s *Store) Snapshot() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, rec)
	}
	return out
}
