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
	LogUDS bool
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
	if !s.LogUDS {
		err := s.Agent.Run(ctx, s.Out)
		if err != nil {
			s.Log.Error().Err(err).Msg("metrics reader error")
		}
		return err
	}

	internal := make(chan domain.MetricSample, 200)
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Agent.Run(ctx, internal)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if err != nil {
				s.Log.Error().Err(err).Msg("metrics reader error")
			}
			return err
		case sample := <-internal:
			s.Log.Info().RawJSON("metrics", []byte(sample.RawJSON)).Msg("metrics uds")
			s.Out <- sample
		}
	}
}
