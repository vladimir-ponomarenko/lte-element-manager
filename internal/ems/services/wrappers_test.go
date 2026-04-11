package services

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
)

func TestMetricsConsumer_Run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	in := make(chan domain.MetricSample)
	b := bus.New(10)
	s := NewMetricsConsumer(in, b, nil, zerolog.Nop())
	if err := s.Run(ctx); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestMetricsLogger_Run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := bus.New(10)
	s := NewMetricsLogger(b, zerolog.Nop())

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}
