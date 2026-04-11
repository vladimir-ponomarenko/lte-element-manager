package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/telemetry"
)

type TelemetryCache struct {
	Bus   *bus.Bus
	Store *telemetry.Store
	Log   zerolog.Logger
}

func NewTelemetryCache(b *bus.Bus, store *telemetry.Store, log zerolog.Logger) *TelemetryCache {
	return &TelemetryCache{Bus: b, Store: store, Log: log}
}

func (s *TelemetryCache) Name() string { return "telemetry_cache" }

func (s *TelemetryCache) Run(ctx context.Context) error {
	if s.Bus == nil || s.Store == nil {
		return nil
	}
	sub := s.Bus.Subscribe(ctx)
	for msg := range sub {
		evt, ok := msg.(telemetry.Event)
		if !ok {
			continue
		}
		s.Store.Update(evt.Samples)
		s.Log.Debug().Int("samples", len(evt.Samples)).Msg("telemetry cached")
	}
	return nil
}
