package netconfcm

import "testing"

func TestLockManagerLifecycle(t *testing.T) {
	m := newLockManager()
	s1 := SessionMeta{SessionID: 1001, Username: "alice"}
	s2 := SessionMeta{SessionID: 1002, Username: "bob"}

	if err := m.lock("candidate", s1); err != nil {
		t.Fatalf("lock candidate by session1: %v", err)
	}
	if err := m.lock("candidate", s1); err != nil {
		t.Fatalf("re-lock by owner should succeed: %v", err)
	}
	if err := m.ensure("candidate", s1); err != nil {
		t.Fatalf("ensure by owner should succeed: %v", err)
	}
	if err := m.lock("candidate", s2); err == nil {
		t.Fatalf("expected second session lock conflict")
	}
	if err := m.ensure("candidate", s2); err == nil {
		t.Fatalf("expected ensure conflict for second session")
	}
	if err := m.unlock("candidate", s2); err == nil {
		t.Fatalf("expected unlock conflict for second session")
	}
	if err := m.unlock("candidate", s1); err != nil {
		t.Fatalf("unlock by owner: %v", err)
	}
	if err := m.lock("candidate", s2); err != nil {
		t.Fatalf("lock after unlock should succeed: %v", err)
	}
	m.releaseSession(s2.SessionID)
	if err := m.lock("candidate", s1); err != nil {
		t.Fatalf("lock after releaseSession should succeed: %v", err)
	}
}

func TestNormalizeDatastoreRejectsUnsupported(t *testing.T) {
	if _, err := normalizeDatastore("startup"); err == nil {
		t.Fatalf("expected startup to be rejected")
	}
}
