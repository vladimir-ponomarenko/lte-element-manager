package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"

	emserrors "lte-element-manager/internal/errors"
)

type LifecycleSupervisor interface {
	TriggerRestart(ctx context.Context, target string) error
}

type RestartPlan struct {
	Primary         string
	Dependents      []string
	DelayAfterStart time.Duration
}

type DockerLifecycleSupervisor struct {
	SocketPath string
	APIVersion string
	Timeout    time.Duration
	Endpoint   string
	HTTPClient *http.Client
	Log        zerolog.Logger
	Plans      map[string]RestartPlan
}

func NewDockerLifecycleSupervisor(socketPath string, timeout time.Duration, log zerolog.Logger) *DockerLifecycleSupervisor {
	return &DockerLifecycleSupervisor{
		SocketPath: socketPath,
		APIVersion: "",
		Timeout:    timeout,
		Endpoint:   "http://unix",
		Log:        log,
		Plans:      map[string]RestartPlan{},
	}
}

func (s *DockerLifecycleSupervisor) SetPlans(plans map[string]RestartPlan) {
	if len(plans) == 0 {
		s.Plans = map[string]RestartPlan{}
		return
	}
	out := make(map[string]RestartPlan, len(plans))
	for k, p := range plans {
		target := strings.TrimSpace(k)
		if target == "" {
			continue
		}
		pp := RestartPlan{
			Primary:         strings.TrimSpace(p.Primary),
			Dependents:      make([]string, 0, len(p.Dependents)),
			DelayAfterStart: p.DelayAfterStart,
		}
		for _, d := range p.Dependents {
			dd := strings.TrimSpace(d)
			if dd != "" {
				pp.Dependents = append(pp.Dependents, dd)
			}
		}
		if pp.Primary == "" {
			pp.Primary = target
		}
		out[target] = pp
	}
	s.Plans = out
}

func (s *DockerLifecycleSupervisor) TriggerRestart(ctx context.Context, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return emserrors.New(emserrors.ErrCodeConfig, "restart target is empty",
			emserrors.WithOp("docker_supervisor"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}

	plan := s.planForTarget(target)
	s.Log.Info().
		Str("primary", plan.Primary).
		Strs("dependents", plan.Dependents).
		Dur("delay_after_start", plan.DelayAfterStart).
		Msg("restart plan selected")

	// Ordering:
	// 1) stop primary (eNB)
	// 2) stop dependents (UEs via docker compose)
	// 3) start primary
	// 4) wait
	// 5) start dependents
	if err := s.stopContainer(ctx, plan.Primary); err != nil {
		return err
	}
	for _, dep := range plan.Dependents {
		if err := s.stopContainer(ctx, dep); err != nil {
			return err
		}
	}

	if err := s.startContainer(ctx, plan.Primary); err != nil {
		return err
	}
	if err := s.waitPrimaryReady(ctx, plan.Primary); err != nil {
		return err
	}
	if err := s.waitAfterPrimaryStart(ctx, plan.DelayAfterStart); err != nil {
		return err
	}
	for _, dep := range plan.Dependents {
		if err := s.startContainer(ctx, dep); err != nil {
			return err
		}
	}

	s.Log.Info().
		Str("primary", plan.Primary).
		Strs("dependents", plan.Dependents).
		Msg("restart plan completed")
	return nil
}

func (s *DockerLifecycleSupervisor) planForTarget(target string) RestartPlan {
	if p, ok := s.Plans[target]; ok {
		if p.Primary == "" {
			p.Primary = target
		}
		return p
	}
	return RestartPlan{
		Primary:         target,
		Dependents:      nil,
		DelayAfterStart: 5 * time.Second,
	}
}

func (s *DockerLifecycleSupervisor) waitAfterPrimaryStart(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		d = 5 * time.Second
	}
	select {
	case <-ctx.Done():
		return emserrors.Wrap(ctx.Err(), emserrors.ErrCodeProcess, "start delay interrupted",
			emserrors.WithOp("docker_supervisor"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	case <-time.After(d):
		return nil
	}
}

func (s *DockerLifecycleSupervisor) waitPrimaryReady(ctx context.Context, target string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		running, err := s.isContainerRunning(ctx, target)
		if err == nil && running {
			return nil
		}
		select {
		case <-ctx.Done():
			return emserrors.Wrap(ctx.Err(), emserrors.ErrCodeProcess, "wait primary ready interrupted",
				emserrors.WithOp("docker_supervisor"),
				emserrors.WithSeverity(emserrors.SeverityMajor),
			)
		case <-time.After(500 * time.Millisecond):
		}
	}
	return emserrors.New(
		emserrors.ErrCodeProcess,
		fmt.Sprintf("container %q did not become running in time", target),
		emserrors.WithOp("docker_supervisor"),
		emserrors.WithSeverity(emserrors.SeverityMajor),
	)
}

func (s *DockerLifecycleSupervisor) isContainerRunning(ctx context.Context, target string) (bool, error) {
	client := s.client()
	u := s.inspectURL(target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("inspect status=%d", resp.StatusCode)
	}
	var body struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return false, err
	}
	return body.State.Running, nil
}

func (s *DockerLifecycleSupervisor) stopContainer(ctx context.Context, target string) error {
	err := s.doContainerAction(ctx, target, "stop", map[string]string{"t": strconvSeconds(s.Timeout)})
	if err == nil {
		return nil
	}

	// srsENB can stall on graceful stop; force-kill as fallback.
	s.Log.Warn().Err(err).Str("target", target).Msg("docker stop failed, falling back to kill")
	killErr := s.doContainerAction(ctx, target, "kill", nil)
	if killErr == nil {
		return nil
	}
	return emserrors.New(
		emserrors.ErrCodeProcess,
		fmt.Sprintf("container stop/kill failed for %q: stop=%v; kill=%v", target, err, killErr),
		emserrors.WithOp("docker_supervisor"),
		emserrors.WithSeverity(emserrors.SeverityCritical),
	)
}

func (s *DockerLifecycleSupervisor) startContainer(ctx context.Context, target string) error {
	// Allow RF/ZMQ sockets to settle after stop/kill before starting again.
	select {
	case <-ctx.Done():
		return emserrors.Wrap(ctx.Err(), emserrors.ErrCodeProcess, "start interrupted before cooldown",
			emserrors.WithOp("docker_supervisor"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	case <-time.After(1500 * time.Millisecond):
	}

	var lastErr error
	for i := 0; i < 5; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return emserrors.Wrap(ctx.Err(), emserrors.ErrCodeProcess, "start retry interrupted",
					emserrors.WithOp("docker_supervisor"),
					emserrors.WithSeverity(emserrors.SeverityMajor),
				)
			case <-time.After(1 * time.Second):
			}
		}
		if err := s.doContainerAction(ctx, target, "start", nil); err == nil {
			return nil
		} else {
			lastErr = err
			s.Log.Warn().Err(err).Str("target", target).Int("attempt", i+1).Msg("docker start failed, retrying")
		}
	}
	return emserrors.New(
		emserrors.ErrCodeProcess,
		fmt.Sprintf("container start failed for %q after retries: %v", target, lastErr),
		emserrors.WithOp("docker_supervisor"),
		emserrors.WithSeverity(emserrors.SeverityCritical),
	)
}

func (s *DockerLifecycleSupervisor) doContainerAction(ctx context.Context, target, action string, query map[string]string) error {
	client := s.client()
	actionURL := s.actionURL(target, action, query)

	reqCtx := ctx
	var cancel context.CancelFunc
	if timeout := s.requestTimeout(action); timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, actionURL, nil)
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeInternal, "build docker request failed",
			emserrors.WithOp("docker_supervisor"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}

	resp, err := client.Do(req)
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeNetwork, "docker api request failed",
			emserrors.WithOp("docker_supervisor"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified:
		s.Log.Debug().Str("target", target).Str("action", action).Int("status", resp.StatusCode).Msg("docker action completed")
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = fmt.Sprintf("status=%d", resp.StatusCode)
		}
		return emserrors.New(emserrors.ErrCodeProcess, fmt.Sprintf("docker action %q failed for %q: %s", action, target, msg),
			emserrors.WithOp("docker_supervisor"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
}

func (s *DockerLifecycleSupervisor) requestTimeout(action string) time.Duration {
	base := s.Timeout
	if base <= 0 {
		base = 20 * time.Second
	}
	switch action {
	case "stop":
		// Give daemon extra time beyond graceful-stop window to return API response.
		return base + 15*time.Second
	default:
		if base < 10*time.Second {
			return 10 * time.Second
		}
		return base
	}
}

func (s *DockerLifecycleSupervisor) actionURL(target, action string, query map[string]string) string {
	base := strings.TrimRight(s.Endpoint, "/")
	escaped := url.PathEscape(target)
	apiVersion := strings.Trim(s.APIVersion, "/")
	u := fmt.Sprintf("%s/containers/%s/%s", base, escaped, action)
	if apiVersion != "" {
		u = fmt.Sprintf("%s/%s/containers/%s/%s", base, apiVersion, escaped, action)
	}
	if len(query) == 0 {
		return u
	}
	q := make(url.Values, len(query))
	for k, v := range query {
		q.Set(k, v)
	}
	return u + "?" + q.Encode()
}

func (s *DockerLifecycleSupervisor) inspectURL(target string) string {
	base := strings.TrimRight(s.Endpoint, "/")
	escaped := url.PathEscape(target)
	apiVersion := strings.Trim(s.APIVersion, "/")
	if apiVersion == "" {
		return fmt.Sprintf("%s/containers/%s/json", base, escaped)
	}
	return fmt.Sprintf("%s/%s/containers/%s/json", base, apiVersion, escaped)
}

func (s *DockerLifecycleSupervisor) client() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	socket := s.SocketPath
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: tr}
}

func strconvSeconds(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	secs := int(d.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return fmt.Sprintf("%d", secs)
}
