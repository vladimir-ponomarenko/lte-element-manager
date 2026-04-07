package alarms

import (
	"time"

	"lte-element-manager/internal/ems/domain"
)

// Manager deduplicates alarms, updates the store, and emits alarm events.
type Manager struct {
	Store *Store
}

func NewManager(store *Store) *Manager {
	if store == nil {
		store = NewStore()
	}
	return &Manager{Store: store}
}

func (m *Manager) Raise(at time.Time, component string, health string, alarm domain.Alarm) (Event, bool) {
	rec, changed := m.Store.Upsert(at, component, alarm)
	return Event{
		At:        at,
		Component: component,
		Health:    health,
		Alarm:     domain.Alarm{Code: rec.Key.Code, Message: rec.Message, Severity: rec.Severity},
		Status:    rec.Status,
		Count:     rec.Count,
	}, changed
}

func (m *Manager) ClearComponent(at time.Time, component string, health string) []Event {
	cleared := m.Store.ClearComponent(at, component)
	out := make([]Event, 0, len(cleared))
	for _, rec := range cleared {
		out = append(out, Event{
			At:        at,
			Component: component,
			Health:    health,
			Alarm:     domain.Alarm{Code: rec.Key.Code, Message: rec.Message, Severity: rec.Severity},
			Status:    rec.Status,
			Count:     rec.Count,
		})
	}
	return out
}
