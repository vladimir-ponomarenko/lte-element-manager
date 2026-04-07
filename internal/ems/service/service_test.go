package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type testSvc struct {
	name string
	run  func(ctx context.Context) error
}

func (s testSvc) Name() string                  { return s.name }
func (s testSvc) Run(ctx context.Context) error { return s.run(ctx) }

func TestRunner_ErrorCancelsOthers(t *testing.T) {
	r := NewRunner(zerolog.Nop())

	var canceled atomic.Bool

	r.Add(testSvc{name: "a", run: func(ctx context.Context) error {
		return errors.New("fail")
	}})
	r.Add(testSvc{name: "b", run: func(ctx context.Context) error {
		<-ctx.Done()
		canceled.Store(true)
		return nil
	}})

	err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !canceled.Load() {
		if time.Now().After(deadline) {
			t.Fatalf("expected other service to observe cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunner_ContextCancel(t *testing.T) {
	r := NewRunner(zerolog.Nop())
	r.Add(testSvc{name: "a", run: func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
