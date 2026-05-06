package alarms

import (
	"time"

	"lte-element-manager/internal/ems/domain"
)

// Manager deduplicates alarms, updates the store, and emits alarm events.
type Manager struct {
	Store   *Store
	OnEvent func(Event)
}

func NewManager(store *Store) *Manager {
	if store == nil {
		store = NewStore()
	}
	return &Manager{Store: store}
}

func (m *Manager) Raise(at time.Time, component string, health string, alarm domain.Alarm) (Event, bool) {
	alarm = Normalize(component, alarm.ManagedObjectInstance, alarm)
	rec, changed := m.Store.Upsert(at, component, alarm)
	evt := Event{
		At:        at,
		Component: component,
		Health:    health,
		Alarm:     recordAlarm(rec),
		Status:    rec.Status,
		Count:     rec.Count,
	}
	if changed && m.OnEvent != nil {
		m.OnEvent(evt)
	}
	return evt, changed
}

func (m *Manager) Touch(at time.Time, component string, code string) {
	if m == nil || m.Store == nil {
		return
	}
	m.Store.Touch(at, component, code)
}

func (m *Manager) ClearComponent(at time.Time, component string, health string) []Event {
	cleared := m.Store.ClearComponent(at, component)
	out := make([]Event, 0, len(cleared))
	for _, rec := range cleared {
		evt := Event{
			At:        at,
			Component: component,
			Health:    health,
			Alarm:     recordAlarm(rec),
			Status:    rec.Status,
			Count:     rec.Count,
		}
		if m.OnEvent != nil {
			m.OnEvent(evt)
		}
		out = append(out, evt)
	}
	return out
}

func (m *Manager) Clear(at time.Time, component string, code string, health string) (Event, bool) {
	rec, ok := m.Store.Clear(at, component, code)
	if !ok {
		return Event{}, false
	}
	evt := Event{
		At:        at,
		Component: component,
		Health:    health,
		Alarm:     recordAlarm(rec),
		Status:    rec.Status,
		Count:     rec.Count,
	}
	if m.OnEvent != nil {
		m.OnEvent(evt)
	}
	return evt, true
}

func (m *Manager) Active() []Record {
	if m == nil || m.Store == nil {
		return nil
	}
	return m.Store.Active()
}

func recordAlarm(rec Record) domain.Alarm {
	return domain.Alarm{
		Code:                  rec.Key.Code,
		Message:               rec.Message,
		Severity:              rec.Severity,
		AlarmID:               rec.AlarmID,
		ManagedObjectInstance: rec.ManagedObjectInstance,
		EventType:             rec.EventType,
		ProbableCause:         rec.ProbableCause,
		PerceivedSeverity:     rec.PerceivedSeverity,
		SpecificProblem:       rec.SpecificProblem,
	}
}
