package app

import (
	"context"

	"lte-element-manager/internal/ems/domain"
)

type MetricsSource interface {
	Run(ctx context.Context, out chan<- domain.MetricSample) error
}

type Agent struct {
	Metrics MetricsSource
}

func New(metrics MetricsSource) *Agent {
	return &Agent{Metrics: metrics}
}

func (a *Agent) Run(ctx context.Context, out chan<- domain.MetricSample) error {
	return a.Metrics.Run(ctx, out)
}
