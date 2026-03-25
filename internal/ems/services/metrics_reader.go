package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/app"
	"lte-element-manager/internal/ems/domain"
)

type MetricsReader struct {
	Agent *app.Agent
	Out   chan<- domain.MetricSample
	Log   zerolog.Logger
}

func NewMetricsReader(agent *app.Agent, out chan<- domain.MetricSample, log zerolog.Logger) *MetricsReader {
	return &MetricsReader{
		Agent: agent,
		Out:   out,
		Log:   log,
	}
}

func (s *MetricsReader) Name() string {
	return "metrics_reader"
}

func (s *MetricsReader) Run(ctx context.Context) error {
	err := s.Agent.Run(ctx, s.Out)
	if err != nil {
		s.Log.Error().Err(err).Msg("metrics reader error")
	}
	return err
}
