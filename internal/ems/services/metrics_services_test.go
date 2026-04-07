package services

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/metrics"
)

func TestMetricsServiceWrappers_NameAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := bus.New(10)
	store := metrics.NewStore()
	in := make(chan domain.MetricSample, 1)

	consumer := NewMetricsConsumer(in, b, nil, zerolog.Nop())
	if consumer.Name() != "metrics_consumer" {
		t.Fatalf("unexpected name: %s", consumer.Name())
	}

	logger := NewMetricsLogger(b, zerolog.Nop())
	if logger.Name() != "metrics_logger" {
		t.Fatalf("unexpected name: %s", logger.Name())
	}

	cache := NewMetricsCache(b, store, "", zerolog.Nop())
	if cache.Name() != "metrics_cache" {
		t.Fatalf("unexpected name: %s", cache.Name())
	}

	errCh := make(chan error, 3)
	go func() { errCh <- consumer.Run(ctx) }()
	go func() { errCh <- logger.Run(ctx) }()
	go func() { errCh <- cache.Run(ctx) }()

	in <- domain.MetricSample{RawJSON: `{"type":"enb_metrics","timestamp":1,"enb_serial":"x"}`}
	time.Sleep(20 * time.Millisecond)

	cancel()
	deadline := time.After(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case <-deadline:
			t.Fatalf("timeout")
		case err := <-errCh:
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}
}
