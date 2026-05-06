package alarms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lte-element-manager/internal/ems/domain"
)

// Store keeps active alarms and their history counters in memory.
type Store struct {
	mu             sync.Mutex
	records        map[Key]Record
	persistence    string
	lastPersistErr error
	onPersistError func(error)
}

func NewStore() *Store {
	return &Store{records: map[Key]Record{}}
}

func NewPersistentStore(path string) (*Store, error) {
	store := NewStore()
	if path == "" {
		return store, nil
	}
	store.persistence = path
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) OnPersistError(fn func(error)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPersistError = fn
}

// Upsert marks alarm as active and updates counters.
func (s *Store) Upsert(at time.Time, component string, alarm domain.Alarm) (Record, bool) {
	k := Key{Component: component, Code: alarm.Code}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.records[k]
	if !ok {
		rec = Record{
			Key:                   k,
			Status:                StatusActive,
			AlarmID:               alarm.AlarmID,
			ManagedObjectInstance: alarm.ManagedObjectInstance,
			EventType:             alarm.EventType,
			ProbableCause:         alarm.ProbableCause,
			PerceivedSeverity:     alarm.PerceivedSeverity,
			SpecificProblem:       alarm.SpecificProblem,
			Message:               alarm.Message,
			Severity:              alarm.Severity,
			FirstSeen:             at,
			LastSeen:              at,
			Count:                 1,
		}
		s.records[k] = rec
		s.persistLocked()
		return rec, true
	}

	changed := false
	if rec.Status != StatusActive {
		rec.Status = StatusActive
		rec.FirstSeen = at
		changed = true
	}
	for _, update := range []struct {
		dst *string
		src string
	}{
		{&rec.AlarmID, alarm.AlarmID},
		{&rec.ManagedObjectInstance, alarm.ManagedObjectInstance},
		{&rec.EventType, alarm.EventType},
		{&rec.ProbableCause, alarm.ProbableCause},
		{&rec.PerceivedSeverity, alarm.PerceivedSeverity},
		{&rec.SpecificProblem, alarm.SpecificProblem},
		{&rec.Message, alarm.Message},
		{&rec.Severity, alarm.Severity},
	} {
		if update.src != "" && *update.dst != update.src {
			*update.dst = update.src
			changed = true
		}
	}
	rec.LastSeen = at
	rec.Count++

	s.records[k] = rec
	s.persistLocked()
	return rec, changed
}

// Touch updates LastSeen and Count for an active alarm without changing any
// semantic fields. It is used for deduplicated repeated threshold violations.
func (s *Store) Touch(at time.Time, component, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := Key{Component: component, Code: code}
	rec, ok := s.records[k]
	if !ok || rec.Status != StatusActive {
		return
	}
	rec.LastSeen = at
	rec.Count++
	s.records[k] = rec
	s.persistLocked()
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
		rec.PerceivedSeverity = SeverityCleared
		rec.LastSeen = at
		s.records[k] = rec
		cleared = append(cleared, rec)
	}
	if len(cleared) > 0 {
		s.persistLocked()
	}
	return cleared
}

// Clear marks one active alarm for a component as cleared.
func (s *Store) Clear(at time.Time, component, code string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := Key{Component: component, Code: code}
	rec, ok := s.records[k]
	if !ok || rec.Status == StatusCleared {
		return Record{}, false
	}
	rec.Status = StatusCleared
	rec.PerceivedSeverity = SeverityCleared
	rec.Severity = "cleared"
	rec.LastSeen = at
	s.records[k] = rec
	s.persistLocked()
	return rec, true
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

func (s *Store) LastPersistError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPersistErr
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.persistence)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var records []Record
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	for _, rec := range records {
		if rec.Key.Component == "" || rec.Key.Code == "" {
			continue
		}
		s.records[rec.Key] = rec
	}
	return nil
}

func (s *Store) persistLocked() {
	if s.persistence == "" {
		return
	}
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, rec)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err == nil {
		err = atomicWriteFile(s.persistence, data)
	}
	if err != nil {
		s.lastPersistErr = err
		if s.onPersistError != nil {
			s.onPersistError(err)
		}
		return
	}
	s.lastPersistErr = nil
}

func atomicWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".aal-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *Store) Active() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		if rec.Status == StatusActive {
			out = append(out, rec)
		}
	}
	return out
}
