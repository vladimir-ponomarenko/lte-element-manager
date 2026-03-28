package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/fcaps/metrics"
)

type MetricsCache struct {
	Bus   *bus.Bus
	Store *metrics.Store
	Path  string
	Log   zerolog.Logger
}

func NewMetricsCache(b *bus.Bus, store *metrics.Store, path string, log zerolog.Logger) *MetricsCache {
	return &MetricsCache{Bus: b, Store: store, Path: path, Log: log}
}

func (s *MetricsCache) Name() string {
	return "metrics_cache"
}

func (s *MetricsCache) Run(ctx context.Context) error {
	metrics.Cache(ctx, s.Bus, s.Store, s.Path, s.Log)
	return nil
}
