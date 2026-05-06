package netconfcm

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const DefaultLockTTL = 30 * time.Second

type lockOwner struct {
	SessionID uint64
	Username  string
	LastSeen  time.Time
}

type lockManager struct {
	mu       sync.Mutex
	locks    map[string]lockOwner
	sessions map[uint64]lockOwner
	ttl      time.Duration
}

func newLockManager() *lockManager {
	return &lockManager{
		locks:    make(map[string]lockOwner, 2),
		sessions: make(map[uint64]lockOwner),
		ttl:      DefaultLockTTL,
	}
}

func normalizeDatastore(ds string) (string, error) {
	ds = strings.TrimSpace(ds)
	switch ds {
	case "candidate", "running":
		return ds, nil
	default:
		return "", fmt.Errorf("unsupported datastore %q", ds)
	}
}

func (m *lockManager) lock(ds string, meta SessionMeta) error {
	ds, err := normalizeDatastore(ds)
	if err != nil {
		return NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.expireLocked(now)
	meta = m.touchLocked(meta, now)
	if owner, ok := m.locks[ds]; ok {
		if owner.SessionID == meta.SessionID {
			owner.LastSeen = now
			m.locks[ds] = owner
			return NewLockDenied(fmt.Sprintf("%s datastore is already locked by this session", ds), owner.SessionID)
		}
		return NewLockDenied(fmt.Sprintf("%s datastore is locked by another session", ds), owner.SessionID)
	}
	m.locks[ds] = lockOwner{SessionID: meta.SessionID, Username: strings.TrimSpace(meta.Username), LastSeen: now}
	return nil
}

func (m *lockManager) unlock(ds string, meta SessionMeta) error {
	ds, err := normalizeDatastore(ds)
	if err != nil {
		return NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(time.Now())
	meta = m.touchLocked(meta, time.Now())
	owner, ok := m.locks[ds]
	if !ok {
		return nil
	}
	if owner.SessionID != meta.SessionID {
		return &RPCError{
			Tag:            ErrorTagOperationFailed,
			Type:           "protocol",
			Message:        fmt.Sprintf("%s datastore is locked by another session", ds),
			OwnerSessionID: owner.SessionID,
		}
	}
	delete(m.locks, ds)
	return nil
}

func (m *lockManager) ensure(ds string, meta SessionMeta, conflictTag string) error {
	ds, err := normalizeDatastore(ds)
	if err != nil {
		return NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(time.Now())
	meta = m.touchLocked(meta, time.Now())
	if owner, ok := m.locks[ds]; ok && owner.SessionID != meta.SessionID {
		if conflictTag == ErrorTagInUse {
			return NewInUse(fmt.Sprintf("%s datastore is locked by another session", ds), owner.SessionID)
		}
		return NewLockDenied(fmt.Sprintf("%s datastore is locked by another session", ds), owner.SessionID)
	}
	return nil
}

func (m *lockManager) releaseSession(sessionID uint64) (candidateReleased bool) {
	if sessionID == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	for ds, owner := range m.locks {
		if owner.SessionID == sessionID {
			if ds == "candidate" {
				candidateReleased = true
			}
			delete(m.locks, ds)
		}
	}
	return candidateReleased
}

func (m *lockManager) resetAll() (candidateReleased bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ds := range m.locks {
		if ds == "candidate" {
			candidateReleased = true
		}
		delete(m.locks, ds)
	}
	for id := range m.sessions {
		delete(m.sessions, id)
	}
	return candidateReleased
}

func (m *lockManager) keepAlive(sessions []SessionMeta, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, meta := range sessions {
		if meta.SessionID == 0 {
			continue
		}
		owner := lockOwner{SessionID: meta.SessionID, Username: strings.TrimSpace(meta.Username), LastSeen: now}
		m.sessions[meta.SessionID] = owner
		for ds, current := range m.locks {
			if current.SessionID == meta.SessionID {
				current.LastSeen = now
				if owner.Username != "" {
					current.Username = owner.Username
				}
				m.locks[ds] = current
			}
		}
	}
}

func (m *lockManager) sweep(now time.Time) (expired []uint64, candidateExpired bool) {
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	expiredSet, candidate := m.expireLocked(now)
	for id := range expiredSet {
		expired = append(expired, id)
	}
	return expired, candidate
}

func (m *lockManager) expireLocked(now time.Time) (map[uint64]struct{}, bool) {
	expired := make(map[uint64]struct{})
	if m.ttl <= 0 {
		return expired, false
	}
	candidateExpired := false
	for ds, owner := range m.locks {
		if owner.SessionID == 0 || owner.LastSeen.IsZero() {
			continue
		}
		if now.Sub(owner.LastSeen) <= m.ttl {
			continue
		}
		if ds == "candidate" {
			candidateExpired = true
		}
		expired[owner.SessionID] = struct{}{}
		delete(m.locks, ds)
	}
	for id, owner := range m.sessions {
		if !owner.LastSeen.IsZero() && now.Sub(owner.LastSeen) > m.ttl {
			expired[id] = struct{}{}
			delete(m.sessions, id)
		}
	}
	return expired, candidateExpired
}

func (m *lockManager) touchLocked(meta SessionMeta, now time.Time) SessionMeta {
	if meta.SessionID == 0 {
		return meta
	}
	owner := lockOwner{SessionID: meta.SessionID, Username: strings.TrimSpace(meta.Username), LastSeen: now}
	if existing, ok := m.sessions[meta.SessionID]; ok && owner.Username == "" {
		owner.Username = existing.Username
	}
	m.sessions[meta.SessionID] = owner
	return meta
}
