package app

import (
	"context"
	"testing"

	"lte-element-manager/internal/ems/domain"
)

type testSource struct {
	err error
}

func (s testSource) Run(ctx context.Context, out chan<- domain.MetricSample) error {
	_ = ctx
	_ = out
	return s.err
}

func TestAgent_Run(t *testing.T) {
	a := New(testSource{err: nil})
	if err := a.Run(context.Background(), make(chan domain.MetricSample)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
