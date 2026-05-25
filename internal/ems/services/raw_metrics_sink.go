package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/domain"
)

///////////////////
// FOR TEST ONLY //
///////////////////

type RawMetricsSink struct {
	In     <-chan domain.MetricSample
	Path   string
	Log    zerolog.Logger
	once   sync.Once
	mu     sync.Mutex
	latest string
}

func NewRawMetricsSink(in <-chan domain.MetricSample, path string, log zerolog.Logger) *RawMetricsSink {
	return &RawMetricsSink{In: in, Path: path, Log: log}
}

func (s *RawMetricsSink) Name() string { return "raw_metrics_sink" }

func (s *RawMetricsSink) Run(ctx context.Context) error {
	if s.In == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return s.flush()
		case sample, ok := <-s.In:
			if !ok {
				return s.flush()
			}
			s.mu.Lock()
			s.latest = sample.RawJSON
			s.mu.Unlock()
			if err := s.flush(); err != nil {
				s.Log.Warn().Err(err).Msg("failed to persist latest EPC snapshot")
			}
		}
	}
}

func (s *RawMetricsSink) flush() error {
	s.mu.Lock()
	latest := s.latest
	path := s.Path
	s.mu.Unlock()
	if path == "" || latest == "" {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	payload, err := json.MarshalIndent(json.RawMessage(latest), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
