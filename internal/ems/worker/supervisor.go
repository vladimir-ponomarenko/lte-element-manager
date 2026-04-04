package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Supervisor keeps a worker running and restarts it with backoff on crashes.
type Supervisor struct {
	Worker  Worker
	Backoff Backoff
	Log     zerolog.Logger
}

func (s *Supervisor) Run(ctx context.Context) error {
	if s.Worker == nil {
		return nil
	}
	if s.Backoff == nil {
		s.Backoff = NewExponentialBackoff(1*time.Second, 30*time.Second, 30*time.Second, 0.1)
	}

	for {
		s.Log.Debug().Str("worker", s.Worker.Name()).Msg("worker starting")
		start := time.Now()
		err := s.Worker.Run(ctx)
		uptime := time.Since(start)

		s.Log.Debug().
			Str("worker", s.Worker.Name()).
			Dur("uptime", uptime).
			Err(err).
			Msg("worker exited")

		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err == nil {
			return nil
		}

		delay := s.Backoff.Next(uptime)
		s.Log.Error().
			Err(err).
			Str("worker", s.Worker.Name()).
			Dur("uptime", uptime).
			Dur("backoff", delay).
			Msg("worker crashed, restarting")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}
