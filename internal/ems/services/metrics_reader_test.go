package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/app"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/health"
	emserrors "lte-element-manager/internal/errors"
)

type testSource struct {
	run func(context.Context, chan<- domain.MetricSample) error
}

func (s testSource) Run(ctx context.Context, out chan<- domain.MetricSample) error {
	return s.run(ctx, out)
}

func TestMetricsReader_NoUDSLog_Success(t *testing.T) {
	h := health.New()
	out := make(chan domain.MetricSample, 1)

	a := app.New(testSource{
		run: func(context.Context, chan<- domain.MetricSample) error { return nil },
	})
	r := NewMetricsReader(a, out, zerolog.Nop(), h)
	_ = r.Name()

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	st, ok := h.Component(health.ComponentUDS)
	if !ok || !st.Up {
		t.Fatalf("expected uds to be up")
	}
}

func TestMetricsReader_NoUDSLog_ErrorMarksHealthDown(t *testing.T) {
	h := health.New()
	out := make(chan domain.MetricSample, 1)

	base := errors.New("socket gone")
	a := app.New(testSource{
		run: func(context.Context, chan<- domain.MetricSample) error { return base },
	})
	r := NewMetricsReader(a, out, zerolog.Nop(), h)
	_ = r.Name()

	err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeNetwork {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}

	st, ok := h.Component(health.ComponentUDS)
	if !ok || st.Up || st.LastErr == nil {
		t.Fatalf("expected uds to be down with error")
	}
}

func TestMetricsReader_UDSLog_ForwardsSamples(t *testing.T) {
	h := health.New()
	out := make(chan domain.MetricSample, 1)

	a := app.New(testSource{
		run: func(ctx context.Context, out chan<- domain.MetricSample) error {
			out <- domain.MetricSample{RawJSON: `{"type":"enb_metrics","timestamp":1,"enb_serial":"x"}`}
			<-ctx.Done()
			return nil
		},
	})

	r := NewMetricsReader(a, out, zerolog.Nop(), h)
	r.LogUDS = true
	_ = r.Name()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for forwarded sample")
	case <-out:
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

	st, ok := h.Component(health.ComponentUDS)
	if !ok || !st.Up {
		t.Fatalf("expected uds to be up")
	}
}

func TestMetricsReader_UDSLog_Error(t *testing.T) {
	h := health.New()
	out := make(chan domain.MetricSample, 1)

	base := errors.New("read failed")
	a := app.New(testSource{
		run: func(context.Context, chan<- domain.MetricSample) error { return base },
	})

	r := NewMetricsReader(a, out, zerolog.Nop(), h)
	r.LogUDS = true
	_ = r.Name()

	err := r.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeNetwork {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
	st, ok := h.Component(health.ComponentUDS)
	if !ok || st.Up || st.LastErr == nil {
		t.Fatalf("expected uds to be down")
	}
}

func TestMetricsReader_UDSLog_CleanExitFromErrCh(t *testing.T) {
	out := make(chan domain.MetricSample, 1)
	a := app.New(testSource{
		run: func(context.Context, chan<- domain.MetricSample) error { return nil },
	})
	r := NewMetricsReader(a, out, zerolog.Nop(), nil)
	r.LogUDS = true

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
