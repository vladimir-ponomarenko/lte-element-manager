package netconfcm

import (
	"fmt"
	"strings"
	"sync"
)

type lockOwner struct {
	SessionID uint64
	Username  string
}

type lockManager struct {
	mu    sync.Mutex
	locks map[string]lockOwner
}

func newLockManager() *lockManager {
	return &lockManager{locks: make(map[string]lockOwner, 2)}
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
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner, ok := m.locks[ds]; ok {
		if owner.SessionID == meta.SessionID {
			return nil
		}
		return fmt.Errorf("%s datastore is locked by session %d", ds, owner.SessionID)
	}
	m.locks[ds] = lockOwner{SessionID: meta.SessionID, Username: strings.TrimSpace(meta.Username)}
	return nil
}

func (m *lockManager) unlock(ds string, meta SessionMeta) error {
	ds, err := normalizeDatastore(ds)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	owner, ok := m.locks[ds]
	if !ok {
		return nil
	}
	if owner.SessionID != meta.SessionID {
		return fmt.Errorf("%s datastore is locked by another session", ds)
	}
	delete(m.locks, ds)
	return nil
}

func (m *lockManager) ensure(ds string, meta SessionMeta) error {
	ds, err := normalizeDatastore(ds)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner, ok := m.locks[ds]; ok && owner.SessionID != meta.SessionID {
		return fmt.Errorf("%s datastore is locked by session %d", ds, owner.SessionID)
	}
	return nil
}

func (m *lockManager) releaseSession(sessionID uint64) {
	if sessionID == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for ds, owner := range m.locks {
		if owner.SessionID == sessionID {
			delete(m.locks, ds)
		}
	}
}
