package services

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/health"
	emserrors "lte-element-manager/internal/errors"
)

type zeroBackoff struct{}

func (zeroBackoff) Next(time.Duration) time.Duration { return 0 }

type fixedBackoff time.Duration

func (b fixedBackoff) Next(time.Duration) time.Duration { return time.Duration(b) }

type testWorker struct {
	name string
	run  func(context.Context) error
	n    atomic.Int32
}

func (w *testWorker) Name() string { return w.name }

func (w *testWorker) Run(ctx context.Context) error {
	w.n.Add(1)
	return w.run(ctx)
}

func TestNetconfServer_HealthyOnCleanExit(t *testing.T) {
	h := health.New()
	s := NewNetconfServer(&testWorker{
		name: "nc",
		run: func(context.Context) error {
			return nil
		},
	}, zerolog.Nop(), h)
	s.Backoff = zeroBackoff{}

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	st, ok := h.Component(health.ComponentNetconf)
	if !ok || !st.Up {
		t.Fatalf("expected netconf component to be up")
	}
	if got := h.State(); got != health.StateHealthy {
		t.Fatalf("unexpected state: %s", got)
	}
}

func TestNetconfServer_HealthDegradedOnCrash(t *testing.T) {
	h := health.New()
	fail := errors.New("boom")

	var calls atomic.Int32
	w := &testWorker{
		name: "nc",
		run: func(ctx context.Context) error {
			if calls.Add(1) == 1 {
				return fail
			}
			<-ctx.Done()
			return nil
		},
	}

	s := NewNetconfServer(w, zerolog.Nop(), h)
	s.Backoff = fixedBackoff(200 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		st, ok := h.Component(health.ComponentNetconf)
		if ok && !st.Up && st.LastErr != nil {
			if emserrors.CodeOf(st.LastErr) != emserrors.ErrCodeProcess {
				t.Fatalf("unexpected code: %s", emserrors.CodeOf(st.LastErr))
			}
			if emserrors.SeverityOf(st.LastErr) != emserrors.SeverityCritical {
				t.Fatalf("unexpected severity: %s", emserrors.SeverityOf(st.LastErr))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for health down")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}

	if got := h.State(); got != health.StateDegraded {
		t.Fatalf("unexpected state: %s", got)
	}
}
