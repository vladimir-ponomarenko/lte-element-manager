package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDockerLifecycleSupervisor_TriggerRestart(t *testing.T) {
	var stopCalled, startCalled, inspectCalled int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.41/containers/ENB-1/stop":
			stopCalled++
			if got := r.URL.Query().Get("t"); got != "5" {
				t.Fatalf("unexpected timeout query: %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1.41/containers/ENB-1/start":
			startCalled++
			w.WriteHeader(http.StatusNoContent)
		case "/v1.41/containers/ENB-1/json":
			inspectCalled++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"State":{"Running":true}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	sup := &DockerLifecycleSupervisor{
		APIVersion: "v1.41",
		Timeout:    5 * time.Second,
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
		Log:        zerolog.Nop(),
	}
	if err := sup.TriggerRestart(context.Background(), "ENB-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopCalled != 1 || startCalled != 1 || inspectCalled < 1 {
		t.Fatalf("unexpected calls: stop=%d start=%d inspect=%d", stopCalled, startCalled, inspectCalled)
	}
}

func TestDockerLifecycleSupervisor_TriggerRestart_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	sup := &DockerLifecycleSupervisor{
		APIVersion: "v1.41",
		Timeout:    5 * time.Second,
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
		Log:        zerolog.Nop(),
	}
	if err := sup.TriggerRestart(context.Background(), "ENB-1"); err == nil {
		t.Fatalf("expected error")
	}
}
