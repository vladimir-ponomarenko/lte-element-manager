package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/mediation"
	"lte-element-manager/internal/ems/telemetry"
	emserrors "lte-element-manager/internal/errors"
)

type MetricsConsumer struct {
	In     <-chan domain.MetricSample
	Bus    *bus.Bus
	Mapper mediation.Mapper
	Log    zerolog.Logger
}

func NewMetricsConsumer(in <-chan domain.MetricSample, b *bus.Bus, mapper mediation.Mapper, log zerolog.Logger) *MetricsConsumer {
	return &MetricsConsumer{
		In:     in,
		Bus:    b,
		Mapper: mapper,
		Log:    log,
	}
}

func (s *MetricsConsumer) Name() string {
	return "metrics_consumer"
}

func (s *MetricsConsumer) Run(ctx context.Context) error {
	if s.Bus == nil || s.Mapper == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case raw, ok := <-s.In:
			if !ok {
				return nil
			}
			samples, err := s.Mapper.Map(raw.RawJSON)
			if err != nil {
				s.Log.Warn().Err(err).Msg("metrics mapping failed")
				continue
			}
			if len(samples) == 0 {
				s.Log.Warn().
					Err(emserrors.New(emserrors.ErrCodeDataCorrupt, "empty canonical mapping result")).
					Msg("metrics mapping failed")
				continue
			}
			s.Bus.Publish(telemetry.Event{Samples: samples})
		}
	}
}
