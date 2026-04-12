package services

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/configuration"
	"lte-element-manager/internal/ems/worker"
	emserrors "lte-element-manager/internal/errors"
)

type ConfigControl struct {
	Addr       string
	Targets    map[string]string
	Supervisor worker.LifecycleSupervisor
	Store      *configuration.Store
	Log        zerolog.Logger
	mu         sync.Mutex
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

func NewConfigControl(addr string, targets map[string]string, sup worker.LifecycleSupervisor, store *configuration.Store, log zerolog.Logger) *ConfigControl {
	return &ConfigControl{
		Addr:       addr,
		Targets:    copyTargets(targets),
		Supervisor: sup,
		Store:      store,
		Log:        log,
	}
}

func (s *ConfigControl) Name() string { return "config_control" }

func (s *ConfigControl) Run(ctx context.Context) error {
	if strings.TrimSpace(s.Addr) == "" || s.Supervisor == nil {
		return nil
	}
	ln, err := net.Listen("tcp", s.Addr)
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
	cfg, err := s.Store.Edit(req.Changes)
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
	running, err := s.Store.Commit()
	if err != nil {
		writeConfigJSON(w, http.StatusBadRequest, configResponse{Status: "error", Message: err.Error()})
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
