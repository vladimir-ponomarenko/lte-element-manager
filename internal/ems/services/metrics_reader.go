package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/app"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/health"
	emserrors "lte-element-manager/internal/errors"
)

type MetricsReader struct {
	Agent  *app.Agent
	Out    chan<- domain.MetricSample
	Log    zerolog.Logger
	LogUDS bool
	Health *health.Tracker
}

func NewMetricsReader(agent *app.Agent, out chan<- domain.MetricSample, log zerolog.Logger, h *health.Tracker) *MetricsReader {
	return &MetricsReader{
		Agent:  agent,
		Out:    out,
		Log:    log,
		Health: h,
	}
}

func (s *MetricsReader) Name() string {
	return "metrics_reader"
}

func (s *MetricsReader) Run(ctx context.Context) error {
	if !s.LogUDS {
		if s.Health != nil {
			s.Health.Up(health.ComponentUDS)
		}
		err := s.Agent.Run(ctx, s.Out)
		if err != nil {
			wrapped := emserrors.Wrap(err, emserrors.ErrCodeNetwork, "metrics socket reader failed",
				emserrors.WithOp("metrics_reader"),
				emserrors.WithSeverity(emserrors.SeverityMajor),
			)
			s.Log.Error().Err(wrapped).Msg("metrics reader error")
			if s.Health != nil {
				s.Health.Down(health.ComponentUDS, wrapped)
			}
			return wrapped
		}
		return nil
	}

	internal := make(chan domain.MetricSample, 200)
	errCh := make(chan error, 1)
	if s.Health != nil {
		s.Health.Up(health.ComponentUDS)
	}
	go func() {
		errCh <- s.Agent.Run(ctx, internal)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if err != nil {
				wrapped := emserrors.Wrap(err, emserrors.ErrCodeNetwork, "metrics socket reader failed",
					emserrors.WithOp("metrics_reader"),
					emserrors.WithSeverity(emserrors.SeverityMajor),
				)
				s.Log.Error().Err(wrapped).Msg("metrics reader error")
				if s.Health != nil {
					s.Health.Down(health.ComponentUDS, wrapped)
				}
				return wrapped
			}
			return err
		case sample := <-internal:
			s.Log.Info().RawJSON("metrics", []byte(sample.RawJSON)).Msg("metrics uds")
			s.Out <- sample
		}
	}
}
