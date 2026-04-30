package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/fcaps/alarms"
)

type fakeLifecycleSupervisor struct {
	target string
	err    error
	done   chan struct{}
}

func (f *fakeLifecycleSupervisor) TriggerRestart(_ context.Context, target string) error {
	f.target = target
	if f.done != nil {
		close(f.done)
	}
	return f.err
}

func TestConfigControl_HandleRestart_OK(t *testing.T) {
	sup := &fakeLifecycleSupervisor{done: make(chan struct{})}
	svc := NewConfigControl("127.0.0.1:0", map[string]string{
		"ENB-0x19A-001-01-SibSutis&Yadro": "ENB-1",
	}, sup, nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(restartRequest{Serial: "ENB-0x19A-001-01-SibSutis&Yadro"})
	req := httptest.NewRequest(http.MethodPost, "/v1/control/restart", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	svc.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	select {
	case <-sup.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("restart was not dispatched")
	}
	if got := sup.target; got != "ENB-1" {
		t.Fatalf("unexpected target: %s", got)
	}
}

func TestConfigControl_HandleRestart_NotFound(t *testing.T) {
	sup := &fakeLifecycleSupervisor{}
	svc := NewConfigControl("127.0.0.1:0", map[string]string{}, sup, nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(restartRequest{Serial: "unknown"})
	req := httptest.NewRequest(http.MethodPost, "/v1/control/restart", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	svc.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
}

func TestConfigControl_HandleRestart_SupervisorError(t *testing.T) {
	sup := &fakeLifecycleSupervisor{err: errors.New("boom"), done: make(chan struct{})}
	svc := NewConfigControl("127.0.0.1:0", map[string]string{
		"ENB-0x19A-001-01-SibSutis&Yadro": "ENB-1",
	}, sup, nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(restartRequest{Serial: "ENB-0x19A-001-01-SibSutis&Yadro"})
	req := httptest.NewRequest(http.MethodPost, "/v1/control/restart", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	svc.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	select {
	case <-sup.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("restart was not dispatched")
	}
}

func TestConfigControl_HandleNetconfNotifications_DrainsQueue(t *testing.T) {
	q := alarms.NewNotificationQueue(10)
	q.Append(alarms.Notification{
		EventTime: time.Date(2026, 5, 6, 1, 2, 3, 0, time.UTC),
		Payload:   `{"ems-fault-management:alarm-notification":{"alarm_id":"Alarm_S1_Down"}}`,
	})
	svc := NewConfigControl("127.0.0.1:0", nil, nil, nil, nil, q, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/v1/control/netconf/notifications", nil)
	rr := httptest.NewRecorder()
	svc.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("Alarm_S1_Down")) {
		t.Fatalf("missing alarm payload: %s", rr.Body.String())
	}
	if q.Len() != 0 {
		t.Fatalf("expected queue to be drained")
	}
}
