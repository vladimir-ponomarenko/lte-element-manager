package alarms

import (
	"path/filepath"
	"testing"
	"time"

	"lte-element-manager/internal/ems/domain"
)

func TestStore_UpsertAndClear(t *testing.T) {
	s := NewStore()

	at1 := time.Unix(1, 0).UTC()
	at2 := time.Unix(2, 0).UTC()
	a := domain.Alarm{Code: "A1", Message: "boom", Severity: "critical"}

	rec, changed := s.Upsert(at1, "uds", a)
	if !changed {
		t.Fatalf("expected changed on first upsert")
	}
	if rec.Status != StatusActive {
		t.Fatalf("expected active, got %s", rec.Status)
	}
	if rec.Count != 1 {
		t.Fatalf("expected count=1, got %d", rec.Count)
	}
	if !rec.FirstSeen.Equal(at1) || !rec.LastSeen.Equal(at1) {
		t.Fatalf("unexpected timestamps: first=%v last=%v", rec.FirstSeen, rec.LastSeen)
	}

	rec, changed = s.Upsert(at2, "uds", a)
	if changed {
		t.Fatalf("expected no material change for identical alarm")
	}
	if rec.Count != 2 {
		t.Fatalf("expected count=2, got %d", rec.Count)
	}
	if !rec.LastSeen.Equal(at2) {
		t.Fatalf("expected lastSeen=at2, got %v", rec.LastSeen)
	}

	cleared := s.ClearComponent(at2, "uds")
	if len(cleared) != 1 {
		t.Fatalf("expected 1 cleared record, got %d", len(cleared))
	}
	if cleared[0].Status != StatusCleared {
		t.Fatalf("expected cleared status, got %s", cleared[0].Status)
	}

	// Reactivate with updated fields.
	rec, changed = s.Upsert(at2, "uds", domain.Alarm{Code: "A1", Message: "boom2", Severity: "minor"})
	if !changed {
		t.Fatalf("expected change on message/severity update")
	}
	if rec.Status != StatusActive {
		t.Fatalf("expected active")
	}

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 snapshot record, got %d", len(snap))
	}
	active := s.Active()
	if len(active) != 1 {
		t.Fatalf("expected 1 active record, got %d", len(active))
	}
}

func TestPersistentStoreReloadsActiveAlarms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aal_state.json")
	s, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("NewPersistentStore: %v", err)
	}
	at := time.Unix(1, 0).UTC()
	_, _ = s.Upsert(at, "uds", Normalize("uds", "SubNetwork=srsRAN/ManagedElement=enb1", domain.Alarm{Message: "socket gone"}))

	reloaded, err := NewPersistentStore(path)
	if err != nil {
		t.Fatalf("reload persistent store: %v", err)
	}
	active := reloaded.Active()
	if len(active) != 1 {
		t.Fatalf("expected one active alarm after reload, got %d", len(active))
	}
	if active[0].Key.Code != AlarmUDSDisconnected || active[0].Count != 1 {
		t.Fatalf("unexpected active alarm: %+v", active[0])
	}
}

func TestDictionary_NormalizeKnownAlarm(t *testing.T) {
	a := Normalize("uds", "SubNetwork=srsRAN/ManagedElement=enb1", domain.Alarm{Message: "socket gone"})
	if a.Code != AlarmUDSDisconnected {
		t.Fatalf("unexpected code: %s", a.Code)
	}
	if a.EventType != EventTypeCommunicationsAlarm {
		t.Fatalf("unexpected event type: %s", a.EventType)
	}
	if a.ProbableCause != ProbableCauseLANError {
		t.Fatalf("unexpected probable cause: %s", a.ProbableCause)
	}
	if a.PerceivedSeverity != SeverityCritical {
		t.Fatalf("unexpected severity: %s", a.PerceivedSeverity)
	}
}
