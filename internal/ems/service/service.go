package service

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

type Service interface {
	Name() string
	Run(ctx context.Context) error
}

type Runner struct {
	services []Service
	log      zerolog.Logger
}

func NewRunner(log zerolog.Logger) *Runner {
	return &Runner{log: log}
}

func (r *Runner) Add(s Service) {
	r.services = append(r.services, s)
}

func (r *Runner) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	for _, svc := range r.services {
		s := svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.log.Info().Str("service", s.Name()).Msg("service started")
			if err := s.Run(ctx); err != nil {
				r.log.Error().Err(err).Str("service", s.Name()).Msg("service error")
				select {
				case errCh <- err:
				default:
				}
				return
			}
			r.log.Info().Str("service", s.Name()).Msg("service stopped")
		}()
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	}

	wg.Wait()
	return nil
}
