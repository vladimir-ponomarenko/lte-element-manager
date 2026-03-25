package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/fcaps/metrics"
)

type MetricsLogger struct {
	Bus *bus.Bus
	Log zerolog.Logger
}

func NewMetricsLogger(b *bus.Bus, log zerolog.Logger) *MetricsLogger {
	return &MetricsLogger{
		Bus: b,
		Log: log,
	}
}

func (s *MetricsLogger) Name() string {
	return "metrics_logger"
}

func (s *MetricsLogger) Run(ctx context.Context) error {
	metrics.Log(ctx, s.Bus, s.Log)
	return nil
}
