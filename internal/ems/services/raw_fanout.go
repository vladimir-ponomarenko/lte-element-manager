package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/domain"
)

type RawFanout struct {
	In   <-chan domain.MetricSample
	Out1 chan<- domain.MetricSample
	Out2 chan<- domain.MetricSample
	Log  zerolog.Logger
}

func NewRawFanout(in <-chan domain.MetricSample, out1, out2 chan<- domain.MetricSample, log zerolog.Logger) *RawFanout {
	return &RawFanout{In: in, Out1: out1, Out2: out2, Log: log}
}

func (s *RawFanout) Name() string { return "raw_fanout" }

func (s *RawFanout) Run(ctx context.Context) error {
	if s.In == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case sample, ok := <-s.In:
			if !ok {
				return nil
			}
			if s.Out1 != nil {
				select {
				case s.Out1 <- sample:
				default:
					s.Log.Warn().Msg("raw fanout dropped sample (out1 full)")
				}
			}
			if s.Out2 != nil {
				select {
				case s.Out2 <- sample:
				default:
					s.Log.Warn().Msg("raw fanout dropped sample (out2 full)")
				}
			}
		}
	}
}
