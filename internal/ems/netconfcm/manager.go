package netconfcm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	if err := m.locks.ensure("candidate", req.SessionMeta); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Target) != "candidate" {
		return nil, fmt.Errorf("only target candidate is supported")
	}
	if err := validateEditOptions(req.DefaultOperation, req.TestOption, req.ErrorOption); err != nil {
		return nil, err
	}
	changes, err := m.extractChanges(req.Payload)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("no editable config true leaves found in edit-config payload")
	}
	if strings.TrimSpace(req.TestOption) == "test-only" {
		cfg, err := m.store.PreviewEdit(changes)
		if err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	cfg, err := m.store.Edit(changes)
	if err != nil {
		return nil, err
	}
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *Manager) Validate(req ValidateRequest) error {
	if req.Payload != "" {
		changes, err := m.extractChanges(req.Payload)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return fmt.Errorf("no editable config true leaves found in validate payload")
		}
		_, err = m.store.PreviewEdit(changes)
		return err
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "candidate"
	}
	switch source {
	case "candidate":
		if err := m.locks.ensure("candidate", req.SessionMeta); err != nil {
			return err
		}
		return configuration.ValidateConfig(m.store.Candidate())
	case "running":
		if err := m.locks.ensure("running", req.SessionMeta); err != nil {
			return err
		}
		return configuration.ValidateConfig(m.store.Running())
	default:
		return fmt.Errorf("unsupported validate source %q", source)
	}
}

func (m *Manager) Lock(req LockRequest) error {
	return m.locks.lock(req.Target, req.SessionMeta)
}

func (m *Manager) Unlock(req LockRequest) error {
	return m.locks.unlock(req.Target, req.SessionMeta)
}

func (m *Manager) DiscardChanges(req SessionMeta) (*configuration.EditableConfig, error) {
	if err := m.locks.ensure("candidate", req); err != nil {
		return nil, err
	}
	m.store.ResetCandidate()
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	cfg := m.store.Candidate()
	return &cfg, nil
}

func (m *Manager) Commit(req CommitRequest) (*configuration.EditableConfig, error) {
	if err := m.locks.ensure("candidate", req.SessionMeta); err != nil {
		return nil, err
	}
	if err := m.locks.ensure("running", req.SessionMeta); err != nil {
		return nil, err
	}
	if err := configuration.ValidateConfig(m.store.Candidate()); err != nil {
		return nil, err
	}

	backup, next, err := m.store.PersistCandidate()
	if err != nil {
		return nil, err
	}

	serial := strings.TrimSpace(next.ENBSerial)
	target, ok := "", false
	if m.resolve != nil {
		target, ok = m.resolve(serial)
	}
	if ok && strings.TrimSpace(target) != "" && m.supervisor != nil {
		if err := m.supervisor.TriggerRestart(context.Background(), target); err != nil {
			_ = m.store.RollbackPersist(backup)
			_ = m.RefreshArtifacts()
			return nil, err
		}
	}
	if err := m.store.FinalizeCommit(); err != nil {
		_ = m.store.RollbackPersist(backup)
		_ = m.RefreshArtifacts()
		return nil, err
	}
	if err := m.RefreshArtifacts(); err != nil {
		return nil, err
	}
	running := m.store.Running()
	return &running, nil
}

func (m *Manager) SessionClose(sessionID uint64) {
	m.locks.releaseSession(sessionID)
}

func (m *Manager) extractChanges(payload string) (map[string]any, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("payload is empty")
	}
	if err := validateYANGJSON(m.yangDir, payload); err != nil {
		return nil, err
	}
	leaves, err := extractLeaves(m.yangDir, payload)
	if err != nil {
		return nil, err
	}
	candidate := m.store.Candidate()
	changes := make(map[string]any)
	for _, leaf := range leaves {
		key, value, structural, err := m.registry.resolve(leaf.Path, leaf.IsKey, leaf.Value)
		if err != nil {
			return nil, err
		}
		if structural || key == "" {
			continue
		}
		if current, ok := m.registry.currentValue(candidate, key); ok && valuesEqual(current, value) {
			continue
		}
		changes[key] = value
	}
	return changes, nil
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
