package alarms

import (
	"testing"
	"time"

	"lte-element-manager/internal/ems/domain"
)

func TestManager_RaiseAndClear(t *testing.T) {
	m := NewManager(NewStore())
	at := time.Unix(1, 0).UTC()

	evt, changed := m.Raise(at, "uds", "degraded", domain.Alarm{Code: "A", Message: "m", Severity: "major"})
	if !changed {
		t.Fatalf("expected changed")
	}
	if evt.Component != "uds" || evt.Alarm.Code != AlarmUDSDisconnected || evt.Status != StatusActive {
		t.Fatalf("unexpected evt: %+v", evt)
	}
	if evt.Alarm.EventType != EventTypeCommunicationsAlarm || evt.Alarm.PerceivedSeverity != SeverityCritical {
		t.Fatalf("unexpected normalized alarm: %+v", evt.Alarm)
	}

	out := m.ClearComponent(at, "uds", "healthy")
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if out[0].Status != StatusCleared {
		t.Fatalf("expected cleared")
	}
	if len(m.Active()) != 0 {
		t.Fatalf("expected no active alarms after clear")
	}
}
