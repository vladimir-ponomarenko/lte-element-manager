package netconfcm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/configuration"
	"lte-element-manager/internal/ems/netconf"
	"lte-element-manager/internal/ems/worker"
)

type targetResolver func(serial string) (string, bool)

type Manager struct {
	yangDir    string
	ids        IDs
	artifacts  ArtifactPaths
	store      *configuration.Store
	supervisor worker.LifecycleSupervisor
	resolve    targetResolver
	locks      *lockManager
	registry   *registry
	log        zerolog.Logger

	mu              sync.Mutex
	lastEditSession uint64
}

func NewManager(yangDir string, ids IDs, artifacts ArtifactPaths, store *configuration.Store, supervisor worker.LifecycleSupervisor, resolve targetResolver, log zerolog.Logger) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("configuration store is not configured")
	}
	m := &Manager{
		yangDir:    strings.TrimSpace(yangDir),
		ids:        ids,
		artifacts:  artifacts,
		store:      store,
		supervisor: supervisor,
		resolve:    resolve,
		locks:      newLockManager(),
		registry:   newRegistry(),
		log:        log,
	}
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) RefreshArtifacts() error {
	if m == nil {
		return nil
	}
	running := m.store.Running()
	candidate := m.store.Candidate()
	if err := m.writeArtifact(m.artifacts.Running, running); err != nil {
		return err
	}
	if err := m.writeArtifact(m.artifacts.Candidate, candidate); err != nil {
		return err
	}
	return nil
}

func (m *Manager) EditFlat(changes map[string]any) (*configuration.EditableConfig, error) {
	cfg, err := m.store.Edit(changes)
	if err != nil {
		return nil, err
	}
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *Manager) EditConfig(req EditRequest) (*configuration.EditableConfig, error) {
	if err := m.locks.ensure("candidate", req.SessionMeta, ErrorTagInUse); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Target) != "candidate" {
		return nil, NewRPCError(ErrorTagInvalidValue, "only target candidate is supported")
	}
	if err := validateEditOptions(req.DefaultOperation, req.TestOption, req.ErrorOption); err != nil {
		return nil, NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	changes, leafCount, err := m.extractChanges(req.Payload)
	if err != nil {
		return nil, err
	}
	if leafCount == 0 {
		return nil, NewRPCError(ErrorTagInvalidValue, "no editable config true leaves found in edit-config payload")
	}
	if len(changes) == 0 {
		cfg := m.store.Candidate()
		return &cfg, nil
	}
	if strings.TrimSpace(req.TestOption) == "test-only" {
		cfg, err := m.store.PreviewEdit(changes)
		if err != nil {
			return nil, NewRPCError(ErrorTagInvalidValue, err.Error())
		}
		return &cfg, nil
	}
	cfg, err := m.store.Edit(changes)
	if err != nil {
		return nil, NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	m.noteCandidateEdit(req.SessionID)
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *Manager) Validate(req ValidateRequest) error {
	if req.Payload != "" {
		changes, leafCount, err := m.extractChanges(req.Payload)
		if err != nil {
			return err
		}
		if leafCount == 0 {
			return NewRPCError(ErrorTagInvalidValue, "no editable config true leaves found in validate payload")
		}
		if len(changes) == 0 {
			return nil
		}
		_, err = m.store.PreviewEdit(changes)
		if err != nil {
			return NewRPCError(ErrorTagInvalidValue, err.Error())
		}
		return nil
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "candidate"
	}
	switch source {
	case "candidate":
		if err := configuration.ValidateConfig(m.store.Candidate()); err != nil {
			return NewRPCError(ErrorTagInvalidValue, err.Error())
		}
		return nil
	case "running":
		if err := configuration.ValidateConfig(m.store.Running()); err != nil {
			return NewRPCError(ErrorTagInvalidValue, err.Error())
		}
		return nil
	default:
		return NewRPCError(ErrorTagInvalidValue, fmt.Sprintf("unsupported validate source %q", source))
	}
}

func (m *Manager) Lock(req LockRequest) error {
	return m.locks.lock(req.Target, req.SessionMeta)
}

func (m *Manager) Unlock(req LockRequest) error {
	if err := m.locks.unlock(req.Target, req.SessionMeta); err != nil {
		return err
	}
	m.store.ResetCandidate()
	m.clearCandidateEditOwner()
	return m.RefreshArtifacts()
}

func (m *Manager) DiscardChanges(req SessionMeta) (*configuration.EditableConfig, error) {
	if err := m.locks.ensure("candidate", req, ErrorTagInUse); err != nil {
		return nil, err
	}
	m.store.ResetCandidate()
	m.clearCandidateEditOwner()
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	cfg := m.store.Candidate()
	return &cfg, nil
}

func (m *Manager) Commit(req CommitRequest) (*configuration.EditableConfig, error) {
	if err := m.locks.ensure("candidate", req.SessionMeta, ErrorTagInUse); err != nil {
		return nil, err
	}
	if err := m.locks.ensure("running", req.SessionMeta, ErrorTagLockDenied); err != nil {
		return nil, err
	}
	if err := configuration.ValidateConfig(m.store.Candidate()); err != nil {
		return nil, NewRPCError(ErrorTagInvalidValue, err.Error())
	}

	backup, next, err := m.store.PersistCandidate()
	if err != nil {
		return nil, NewRPCError(ErrorTagOperationFailed, err.Error())
	}
	needsRestart := backup != nil && backup.Dirty

	serial := strings.TrimSpace(next.ENBSerial)
	target, ok := "", false
	if m.resolve != nil {
		target, ok = m.resolve(serial)
	}
	if needsRestart && ok && strings.TrimSpace(target) != "" && m.supervisor != nil {
		if err := m.supervisor.TriggerRestart(context.Background(), target); err != nil {
			_ = m.store.RollbackPersist(backup)
			m.store.ResetCandidate()
			m.clearCandidateEditOwner()
			_ = m.RefreshArtifacts()
			return nil, err
		}
	}
	if err := m.store.FinalizeCommit(); err != nil {
		_ = m.store.RollbackPersist(backup)
		m.store.ResetCandidate()
		m.clearCandidateEditOwner()
		_ = m.RefreshArtifacts()
		return nil, err
	}
	m.store.ResetCandidate()
	m.clearCandidateEditOwner()
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	running := m.store.Running()
	return &running, nil
}

func (m *Manager) SessionClose(sessionID uint64) {
	if m.locks.releaseSession(sessionID) || m.consumeCandidateEditOwner(sessionID) {
		m.store.ResetCandidate()
		if err := m.RefreshArtifacts(); err != nil {
			m.log.Warn().Err(err).Uint64("session_id", sessionID).Msg("failed to refresh artifacts after session close")
		}
	}
}

func (m *Manager) ResetSessions() {
	m.locks.resetAll()
	m.clearCandidateEditOwner()
	m.store.ResetCandidate()
	if err := m.RefreshArtifacts(); err != nil {
		m.log.Warn().Err(err).Msg("failed to refresh artifacts after NETCONF session reset")
	}
}

func (m *Manager) KeepAlive(sessions []SessionMeta) {
	m.locks.keepAlive(sessions, time.Now())
}

func (m *Manager) SweepExpiredSessions(now time.Time) []uint64 {
	expired, candidateExpired := m.locks.sweep(now)
	if candidateExpired || m.expiredCandidateEditOwner(expired) {
		m.store.ResetCandidate()
		if err := m.RefreshArtifacts(); err != nil {
			m.log.Warn().Err(err).Msg("failed to refresh artifacts after stale candidate lock cleanup")
		}
	}
	for _, id := range expired {
		m.log.Warn().Uint64("session_id", id).Msg("released stale NETCONF session lock")
	}
	return expired
}

func (m *Manager) noteCandidateEdit(sessionID uint64) {
	if sessionID == 0 {
		return
	}
	m.mu.Lock()
	m.lastEditSession = sessionID
	m.mu.Unlock()
}

func (m *Manager) clearCandidateEditOwner() {
	m.mu.Lock()
	m.lastEditSession = 0
	m.mu.Unlock()
}

func (m *Manager) consumeCandidateEditOwner(sessionID uint64) bool {
	if sessionID == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastEditSession != sessionID {
		return false
	}
	m.lastEditSession = 0
	return true
}

func (m *Manager) expiredCandidateEditOwner(expired []uint64) bool {
	if len(expired) == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range expired {
		if id != 0 && id == m.lastEditSession {
			m.lastEditSession = 0
			return true
		}
	}
	return false
}

func (m *Manager) extractChanges(payload string) (map[string]any, int, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, 0, NewRPCError(ErrorTagInvalidValue, "payload is empty")
	}
	if err := validateYANGJSON(m.yangDir, payload); err != nil {
		return nil, 0, NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	leaves, err := extractLeaves(m.yangDir, payload)
	if err != nil {
		return nil, 0, NewRPCError(ErrorTagInvalidValue, err.Error())
	}
	candidate := m.store.Candidate()
	changes := make(map[string]any)
	editableLeaves := 0
	for _, leaf := range leaves {
		key, value, structural, err := m.registry.resolve(leaf.Path, leaf.IsKey, leaf.Value)
		if err != nil {
			return nil, 0, NewRPCError(ErrorTagInvalidValue, err.Error())
		}
		if structural || key == "" {
			continue
		}
		editableLeaves++
		if current, ok := m.registry.currentValue(candidate, key); ok && valuesEqual(current, value) {
			continue
		}
		changes[key] = value
	}
	return changes, editableLeaves, nil
}

func (m *Manager) writeArtifact(path string, cfg configuration.EditableConfig) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data := m.registry.render(m.ids, cfg)
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := validateYANGJSON(m.yangDir, string(raw)); err != nil {
		return fmt.Errorf("artifact %s failed YANG validation: %w", path, err)
	}
	return netconf.WriteSnapshotFile(path, raw)
}

func validateEditOptions(defaultOperation, testOption, errorOption string) error {
	switch strings.TrimSpace(defaultOperation) {
	case "", "merge", "replace", "none":
	default:
		return fmt.Errorf("unsupported default-operation %q", defaultOperation)
	}
	switch strings.TrimSpace(testOption) {
	case "", "set", "test-then-set", "test-only":
	default:
		return fmt.Errorf("unsupported test-option %q", testOption)
	}
	switch strings.TrimSpace(errorOption) {
	case "", "stop-on-error", "rollback-on-error":
	default:
		return fmt.Errorf("unsupported error-option %q", errorOption)
	}
	return nil
}

func valuesEqual(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int32:
		bv, ok := b.(int32)
		return ok && av == bv
	case uint32:
		bv, ok := b.(uint32)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return false
	}
}
