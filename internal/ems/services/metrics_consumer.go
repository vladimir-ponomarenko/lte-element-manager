package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/metrics"
)

type MetricsConsumer struct {
	In     <-chan domain.MetricSample
	Bus    *bus.Bus
	Parser metrics.ParseFunc
	Log    zerolog.Logger
}

func NewMetricsConsumer(in <-chan domain.MetricSample, b *bus.Bus, parser metrics.ParseFunc, log zerolog.Logger) *MetricsConsumer {
	return &MetricsConsumer{
		In:     in,
		Bus:    b,
		Parser: parser,
		Log:    log,
	}
}

func (s *MetricsConsumer) Name() string {
	return "metrics_consumer"
}

func (s *MetricsConsumer) Run(ctx context.Context) error {
	metrics.Consume(ctx, s.In, s.Bus, s.Parser, s.Log)
	return nil
}
