package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/configuration"
	"lte-element-manager/internal/ems/fcaps/alarms"
	"lte-element-manager/internal/ems/netconfcm"
	"lte-element-manager/internal/ems/worker"
	emserrors "lte-element-manager/internal/errors"
)

type ConfigControl struct {
	Addr          string
	Targets       map[string]string
	Supervisor    worker.LifecycleSupervisor
	Store         *configuration.Store
	NETCONF       *netconfcm.Manager
	Notifications *alarms.NotificationQueue
	Log           zerolog.Logger
	mu            sync.Mutex
}

type restartRequest struct {
	Serial string `json:"serial"`
}

type restartResponse struct {
	Status    string `json:"status"`
	Serial    string `json:"serial"`
	Container string `json:"container,omitempty"`
	Message   string `json:"message,omitempty"`
}

type editConfigRequest struct {
	Changes map[string]any `json:"changes"`
}

type configResponse struct {
	Status    string                        `json:"status"`
	Running   *configuration.EditableConfig `json:"running,omitempty"`
	Candidate *configuration.EditableConfig `json:"candidate,omitempty"`
	Message   string                        `json:"message,omitempty"`
}

func NewConfigControl(addr string, targets map[string]string, sup worker.LifecycleSupervisor, store *configuration.Store, netconfMgr *netconfcm.Manager, notifications *alarms.NotificationQueue, log zerolog.Logger) *ConfigControl {
	return &ConfigControl{
		Addr:          addr,
		Targets:       copyTargets(targets),
		Supervisor:    sup,
		Store:         store,
		NETCONF:       netconfMgr,
		Notifications: notifications,
		Log:           log,
	}
}

func (s *ConfigControl) Name() string { return "config_control" }

func (s *ConfigControl) Run(ctx context.Context) error {
	if strings.TrimSpace(s.Addr) == "" {
		return nil
	}
	if s.Supervisor == nil && s.Store == nil && s.NETCONF == nil {
		return nil
	}

	network := "tcp"
	addr := s.Addr
	if strings.HasPrefix(strings.TrimSpace(s.Addr), "unix:") {
		network = "unix"
		addr = strings.TrimPrefix(strings.TrimSpace(s.Addr), "unix:")
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return emserrors.New(emserrors.ErrCodeConfig, "control addr unix path is empty",
				emserrors.WithOp("config_control"),
				emserrors.WithSeverity(emserrors.SeverityCritical),
			)
		}
		// Remove stale socket file from previous runs.
		_ = os.Remove(addr)
	}

	ln, err := net.Listen(network, addr)
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeNetwork, "control listen failed",
			emserrors.WithOp("config_control"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	defer ln.Close()

	srv := &http.Server{Handler: s.handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	err = srv.Serve(ln)
	if err == nil || err == http.ErrServerClosed || ctx.Err() != nil {
		return nil
	}
	if network == "unix" && errors.Is(err, net.ErrClosed) {
		return nil
	}
	return emserrors.Wrap(err, emserrors.ErrCodeNetwork, "control server failed",
		emserrors.WithOp("config_control"),
		emserrors.WithSeverity(emserrors.SeverityMajor),
	)
}

func (s *ConfigControl) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/control/restart", s.handleRestart)
	mux.HandleFunc("/v1/control/config/running", s.handleRunning)
	mux.HandleFunc("/v1/control/config/candidate", s.handleCandidate)
	mux.HandleFunc("/v1/control/config/edit-config", s.handleEditConfig)
	mux.HandleFunc("/v1/control/config/commit", s.handleCommit)
	mux.HandleFunc("/v1/control/netconf/edit-config", s.handleNetconfEditConfig)
	mux.HandleFunc("/v1/control/netconf/validate", s.handleNetconfValidate)
	mux.HandleFunc("/v1/control/netconf/commit", s.handleNetconfCommit)
	mux.HandleFunc("/v1/control/netconf/discard-changes", s.handleNetconfDiscardChanges)
	mux.HandleFunc("/v1/control/netconf/lock", s.handleNetconfLock)
	mux.HandleFunc("/v1/control/netconf/unlock", s.handleNetconfUnlock)
	mux.HandleFunc("/v1/control/netconf/session-close", s.handleNetconfSessionClose)
	mux.HandleFunc("/v1/control/netconf/notifications", s.handleNetconfNotifications)
	return mux
}

func (s *ConfigControl) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, restartResponse{
			Status:  "error",
			Message: "method not allowed",
		})
		return
	}
	if s.Supervisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, restartResponse{
			Status:  "error",
			Message: "supervisor is not configured",
		})
		return
	}

	var req restartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, restartResponse{
			Status:  "error",
			Message: "invalid JSON payload",
		})
		return
	}
	serial := strings.TrimSpace(req.Serial)
	if serial == "" {
		writeJSON(w, http.StatusBadRequest, restartResponse{
			Status:  "error",
			Message: "serial is required",
		})
		return
	}

	target, ok := s.resolveTargetForSerial(serial)
	if !ok || strings.TrimSpace(target) == "" {
		writeJSON(w, http.StatusNotFound, restartResponse{
			Status:  "error",
			Serial:  serial,
			Message: "serial is not managed by this EMS instance",
		})
		return
	}

	if !s.mu.TryLock() {
		writeJSON(w, http.StatusConflict, restartResponse{
			Status:    "error",
			Serial:    serial,
			Container: target,
			Message:   "restart already in progress",
		})
		return
	}

	go func(serial, target string) {
		defer s.mu.Unlock()
		if err := s.Supervisor.TriggerRestart(context.Background(), target); err != nil {
			s.Log.Error().Err(err).Str("serial", serial).Str("container", target).Msg("restart failed")
			return
		}
		s.Log.Info().Str("serial", serial).Str("container", target).Msg("restart completed")
	}(serial, target)

	s.Log.Info().Str("serial", serial).Str("container", target).Msg("restart accepted")
	writeJSON(w, http.StatusAccepted, restartResponse{
		Status:    "accepted",
		Serial:    serial,
		Container: target,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload restartResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func copyTargets(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		kk := strings.TrimSpace(k)
		vv := strings.TrimSpace(v)
		if kk == "" || vv == "" {
			continue
		}
		out[kk] = vv
	}
	return out
}

func (s *ConfigControl) handleRunning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.Store == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "configuration store is not configured"})
		return
	}
	cfg := s.Store.Running()
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Running: &cfg})
}

func (s *ConfigControl) handleCandidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.Store == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "configuration store is not configured"})
		return
	}
	cfg := s.Store.Candidate()
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Candidate: &cfg})
}

func (s *ConfigControl) handleEditConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.Store == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "configuration store is not configured"})
		return
	}
	var req editConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	if len(req.Changes) == 0 {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "changes are required"})
		return
	}
	var (
		cfg configuration.EditableConfig
		err error
	)
	if s.NETCONF != nil {
		var out *configuration.EditableConfig
		out, err = s.NETCONF.EditFlat(req.Changes)
		if out != nil {
			cfg = *out
		}
	} else {
		cfg, err = s.Store.Edit(req.Changes)
	}
	if err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Candidate: &cfg})
}

func (s *ConfigControl) handleCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.Store == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "configuration store is not configured"})
		return
	}
	var (
		running configuration.EditableConfig
		err     error
	)
	if s.NETCONF != nil {
		var out *configuration.EditableConfig
		out, err = s.NETCONF.Commit(netconfcm.CommitRequest{})
		if out != nil {
			running = *out
		}
	} else {
		running, err = s.Store.Commit()
	}
	if err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
		return
	}
	if s.NETCONF != nil {
		writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Running: &running})
		return
	}
	serial := strings.TrimSpace(running.ENBSerial)
	target, ok := s.resolveTargetForSerial(serial)
	if !ok || s.Supervisor == nil {
		writeConfigJSON(w, http.StatusOK, configResponse{
			Status:  "ok",
			Running: &running,
			Message: "config committed; restart is skipped (target/supervisor unavailable)",
		})
		return
	}
	if err := s.Supervisor.TriggerRestart(context.Background(), target); err != nil {
		writeConfigJSON(w, http.StatusConflict, configResponse{
			Status:  "error",
			Running: &running,
			Message: err.Error(),
		})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Running: &running})
}

func (s *ConfigControl) resolveTargetForSerial(serial string) (string, bool) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return "", false
	}
	if target, ok := s.Targets[serial]; ok && strings.TrimSpace(target) != "" {
		return target, true
	}

	if s.Store == nil || len(s.Targets) != 1 {
		return "", false
	}

	r := s.Store.Running()
	c := s.Store.Candidate()
	if serial != strings.TrimSpace(r.ENBSerial) && serial != strings.TrimSpace(c.ENBSerial) {
		return "", false
	}
	for _, target := range s.Targets {
		if strings.TrimSpace(target) != "" {
			return target, true
		}
	}
	return "", false
}

func writeConfigJSON(w http.ResponseWriter, status int, payload configResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *ConfigControl) handleNetconfEditConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.NETCONF == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "netconf mediation is not configured"})
		return
	}
	var req netconfcm.EditRequest
	if err := decodeNetconfEditRequest(r, &req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	cfg, err := s.NETCONF.EditConfig(req)
	if err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Candidate: cfg})
}

func (s *ConfigControl) handleNetconfValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.NETCONF == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "netconf mediation is not configured"})
		return
	}
	var req netconfcm.ValidateRequest
	if err := decodeNetconfValidateRequest(r, &req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	if err := s.NETCONF.Validate(req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok"})
}

func (s *ConfigControl) handleNetconfCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.NETCONF == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "netconf mediation is not configured"})
		return
	}
	var req netconfcm.CommitRequest
	if err := decodeNetconfCommitRequest(r, &req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	cfg, err := s.NETCONF.Commit(req)
	if err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Running: cfg})
}

func (s *ConfigControl) handleNetconfDiscardChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.NETCONF == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "netconf mediation is not configured"})
		return
	}
	var req netconfcm.SessionMeta
	if err := decodeNetconfSessionMeta(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	cfg, err := s.NETCONF.DiscardChanges(req)
	if err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok", Candidate: cfg})
}

func (s *ConfigControl) handleNetconfLock(w http.ResponseWriter, r *http.Request) {
	s.handleNetconfLockLike(w, r, true)
}

func (s *ConfigControl) handleNetconfUnlock(w http.ResponseWriter, r *http.Request) {
	s.handleNetconfLockLike(w, r, false)
}

func (s *ConfigControl) handleNetconfLockLike(w http.ResponseWriter, r *http.Request, acquire bool) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.NETCONF == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "netconf mediation is not configured"})
		return
	}
	var req netconfcm.LockRequest
	if err := decodeNetconfLockRequest(r, &req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	var err error
	if acquire {
		err = s.NETCONF.Lock(req)
	} else {
		err = s.NETCONF.Unlock(req)
	}
	if err != nil {
		writeConfigJSON(w, http.StatusConflict, configResponse{Status: "error", Message: err.Error()})
		return
	}
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok"})
}

func (s *ConfigControl) handleNetconfSessionClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.NETCONF == nil {
		writeConfigJSON(w, http.StatusServiceUnavailable, configResponse{Status: "error", Message: "netconf mediation is not configured"})
		return
	}
	var req netconfcm.SessionMeta
	if err := decodeNetconfSessionMeta(r, &req); err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: "invalid JSON payload"})
		return
	}
	s.NETCONF.SessionClose(req.SessionID)
	writeConfigJSON(w, http.StatusOK, configResponse{Status: "ok"})
}

func (s *ConfigControl) handleNetconfNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeConfigJSON(w, http.StatusMethodNotAllowed, configResponse{Status: "error", Message: "method not allowed"})
		return
	}
	if s.Notifications == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	max := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("max")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			max = v
		}
	}
	items := s.Notifications.Drain(max)
	if len(items) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	for _, item := range items {
		_, _ = fmt.Fprintf(w, "%s\t%s\n", item.EventTime.UTC().Format(time.RFC3339), item.Payload)
	}
}

func decodeNetconfEditRequest(r *http.Request, out *netconfcm.EditRequest) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" && strings.HasPrefix(trimmed, "{") {
		var envelope netconfcm.EditRequest
		if err := json.Unmarshal(body, &envelope); err == nil && isNetconfEditEnvelope(envelope) {
			*out = envelope
			return nil
		}
	}
	populateSessionMetaHeaders(r, &out.SessionMeta)
	out.Target = strings.TrimSpace(r.Header.Get("X-NETCONF-Target"))
	out.DefaultOperation = strings.TrimSpace(r.Header.Get("X-NETCONF-Default-Operation"))
	out.TestOption = strings.TrimSpace(r.Header.Get("X-NETCONF-Test-Option"))
	out.ErrorOption = strings.TrimSpace(r.Header.Get("X-NETCONF-Error-Option"))
	out.Payload = trimmed
	if out.Payload == "" {
		return io.EOF
	}
	return nil
}

func decodeNetconfValidateRequest(r *http.Request, out *netconfcm.ValidateRequest) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" && strings.HasPrefix(trimmed, "{") {
		var envelope netconfcm.ValidateRequest
		if err := json.Unmarshal(body, &envelope); err == nil && isNetconfValidateEnvelope(envelope) {
			*out = envelope
			return nil
		}
	}
	populateSessionMetaHeaders(r, &out.SessionMeta)
	out.Source = strings.TrimSpace(r.Header.Get("X-NETCONF-Source"))
	out.Payload = trimmed
	return nil
}

func decodeNetconfCommitRequest(r *http.Request, out *netconfcm.CommitRequest) error {
	var meta netconfcm.SessionMeta
	if err := decodeNetconfSessionMeta(r, &meta); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	out.SessionMeta = meta
	return nil
}

func decodeNetconfLockRequest(r *http.Request, out *netconfcm.LockRequest) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" {
		if err := json.Unmarshal(body, out); err == nil {
			if strings.TrimSpace(out.Target) != "" || out.SessionID != 0 || strings.TrimSpace(out.Username) != "" {
				return nil
			}
		}
	}
	populateSessionMetaHeaders(r, &out.SessionMeta)
	out.Target = strings.TrimSpace(r.Header.Get("X-NETCONF-Target"))
	if out.Target == "" && trimmed == "" {
		return io.EOF
	}
	return nil
}

func decodeNetconfSessionMeta(r *http.Request, out *netconfcm.SessionMeta) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" {
		if err := json.Unmarshal(body, out); err == nil {
			if out.SessionID != 0 || strings.TrimSpace(out.Username) != "" {
				return nil
			}
		}
	}
	populateSessionMetaHeaders(r, out)
	if out.SessionID == 0 && strings.TrimSpace(out.Username) == "" && trimmed == "" {
		return io.EOF
	}
	return nil
}

func populateSessionMetaHeaders(r *http.Request, out *netconfcm.SessionMeta) {
	out.Username = strings.TrimSpace(r.Header.Get("X-NETCONF-Username"))
	if raw := strings.TrimSpace(r.Header.Get("X-NETCONF-Session-ID")); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			out.SessionID = v
		}
	}
}

func isNetconfEditEnvelope(req netconfcm.EditRequest) bool {
	return strings.TrimSpace(req.Payload) != "" ||
		strings.TrimSpace(req.Target) != "" ||
		strings.TrimSpace(req.DefaultOperation) != "" ||
		strings.TrimSpace(req.TestOption) != "" ||
		strings.TrimSpace(req.ErrorOption) != "" ||
		req.SessionID != 0 ||
		strings.TrimSpace(req.Username) != ""
}

func isNetconfValidateEnvelope(req netconfcm.ValidateRequest) bool {
	return strings.TrimSpace(req.Payload) != "" ||
		strings.TrimSpace(req.Source) != "" ||
		req.SessionID != 0 ||
		strings.TrimSpace(req.Username) != ""
}
