package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type backoffFunc func(time.Duration) time.Duration

func (f backoffFunc) Next(uptime time.Duration) time.Duration { return f(uptime) }

type testWorker struct {
	name string
	run  func(context.Context) error
}

func (w testWorker) Name() string { return w.name }

func (w testWorker) Run(ctx context.Context) error { return w.run(ctx) }

func TestSupervisor_NilWorker(t *testing.T) {
	s := &Supervisor{Worker: nil}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSupervisor_DefaultBackoff_DoesNotSleepOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &Supervisor{
		Worker: testWorker{
			name: "w",
			run:  func(context.Context) error { return errors.New("boom") },
		},
		Backoff: nil,
		Log:     zerolog.Nop(),
	}
	if err := s.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSupervisor_ExitOnCleanReturn(t *testing.T) {
	var started atomic.Int32
	var exited atomic.Int32

	s := &Supervisor{
		Worker: testWorker{
			name: "w",
			run:  func(context.Context) error { return nil },
		},
		Backoff: backoffFunc(func(time.Duration) time.Duration { return 0 }),
		Log:     zerolog.Nop(),
		OnStart: func(string) { started.Add(1) },
		OnExit:  func(string, error, time.Duration) { exited.Add(1) },
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started.Load() != 1 || exited.Load() != 1 {
		t.Fatalf("unexpected callbacks: started=%d exited=%d", started.Load(), exited.Load())
	}
}

func TestSupervisor_RestartUntilSuccess(t *testing.T) {
	var calls atomic.Int32
	s := &Supervisor{
		Worker: testWorker{
			name: "w",
			run: func(context.Context) error {
				if calls.Add(1) == 1 {
					return errors.New("boom")
				}
				return nil
			},
		},
		Backoff: backoffFunc(func(time.Duration) time.Duration { return 0 }),
		Log:     zerolog.Nop(),
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 runs, got %d", calls.Load())
	}
}

func TestSupervisor_CancelWhileWaitingBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	s := &Supervisor{
		Worker: testWorker{
			name: "w",
			run: func(context.Context) error {
				calls.Add(1)
				return errors.New("boom")
			},
		},
		Backoff: backoffFunc(func(time.Duration) time.Duration { return time.Second }),
		Log:     zerolog.Nop(),
	}

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	time.AfterFunc(5*time.Millisecond, cancel)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}

	if calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", calls.Load())
	}
}
