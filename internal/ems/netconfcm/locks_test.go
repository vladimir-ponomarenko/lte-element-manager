package netconfcm

import (
	"testing"
	"time"
)

func TestLockManagerLifecycle(t *testing.T) {
	m := newLockManager()
	s1 := SessionMeta{SessionID: 1001, Username: "alice"}
	s2 := SessionMeta{SessionID: 1002, Username: "bob"}

	if err := m.lock("candidate", s1); err != nil {
		t.Fatalf("lock candidate by session1: %v", err)
	}
	if err := m.lock("candidate", s1); err == nil {
		t.Fatalf("expected self re-lock to be denied")
	} else if rpcErr, ok := err.(*RPCError); !ok || rpcErr.Tag != ErrorTagLockDenied || rpcErr.OwnerSessionID != s1.SessionID {
		t.Fatalf("unexpected self re-lock error: %#v", err)
	}
	if err := m.ensure("candidate", s1, ErrorTagInUse); err != nil {
		t.Fatalf("ensure by owner should succeed: %v", err)
	}
	if err := m.lock("candidate", s2); err == nil {
		t.Fatalf("expected second session lock conflict")
	} else if rpcErr, ok := err.(*RPCError); !ok || rpcErr.Tag != ErrorTagLockDenied || rpcErr.OwnerSessionID != s1.SessionID {
		t.Fatalf("unexpected second lock error: %#v", err)
	}
	if err := m.ensure("candidate", s2, ErrorTagInUse); err == nil {
		t.Fatalf("expected ensure conflict for second session")
	} else if rpcErr, ok := err.(*RPCError); !ok || rpcErr.Tag != ErrorTagInUse || rpcErr.OwnerSessionID != s1.SessionID {
		t.Fatalf("unexpected ensure conflict error: %#v", err)
	}
	if err := m.unlock("candidate", s2); err == nil {
		t.Fatalf("expected unlock conflict for second session")
	} else if rpcErr, ok := err.(*RPCError); !ok || rpcErr.Tag != ErrorTagOperationFailed || rpcErr.OwnerSessionID != s1.SessionID {
		t.Fatalf("unexpected unlock conflict error: %#v", err)
	}
	if err := m.unlock("candidate", s1); err != nil {
		t.Fatalf("unlock by owner: %v", err)
	}
	if err := m.lock("candidate", s2); err != nil {
		t.Fatalf("lock after unlock should succeed: %v", err)
	}
	if !m.releaseSession(s2.SessionID) {
		t.Fatalf("expected candidate lock to be released with session")
	}
	if err := m.lock("candidate", s1); err != nil {
		t.Fatalf("lock after releaseSession should succeed: %v", err)
	}
}

func TestNormalizeDatastoreRejectsUnsupported(t *testing.T) {
	if _, err := normalizeDatastore("startup"); err == nil {
		t.Fatalf("expected startup to be rejected")
	}
}

func TestLockManagerExpiresStaleLock(t *testing.T) {
	m := newLockManager()
	m.ttl = 30 * time.Second
	s1 := SessionMeta{SessionID: 1001, Username: "alice"}
	s2 := SessionMeta{SessionID: 1002, Username: "bob"}
	now := time.Unix(100, 0)

	m.keepAlive([]SessionMeta{s1}, now)
	if err := m.lock("candidate", s1); err != nil {
		t.Fatalf("lock candidate: %v", err)
	}
	m.mu.Lock()
	owner := m.locks["candidate"]
	owner.LastSeen = now
	m.locks["candidate"] = owner
	m.mu.Unlock()
	expired, candidateExpired := m.sweep(now.Add(31 * time.Second))
	if !candidateExpired || len(expired) != 1 || expired[0] != s1.SessionID {
		t.Fatalf("unexpected sweep result: expired=%v candidate=%v", expired, candidateExpired)
	}
	if err := m.lock("candidate", s2); err != nil {
		t.Fatalf("lock after stale expiry: %v", err)
	}
}
