package services

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/health"
	"lte-element-manager/internal/ems/worker"
	emserrors "lte-element-manager/internal/errors"
)

type NetconfServer struct {
	Worker  worker.Worker
	Log     zerolog.Logger
	Health  *health.Tracker
	Backoff worker.Backoff
}

func NewNetconfServer(w worker.Worker, log zerolog.Logger, h *health.Tracker) *NetconfServer {
	return &NetconfServer{Worker: w, Log: log, Health: h}
}

func (s *NetconfServer) Name() string {
	return "netconf_server"
}

func (s *NetconfServer) Run(ctx context.Context) error {
	b := s.Backoff
	if b == nil {
		b = worker.NewExponentialBackoff(1*time.Second, 30*time.Second, 30*time.Second, 0.1)
	}
	sup := &worker.Supervisor{
		Worker:  s.Worker,
		Backoff: b,
		Log:     s.Log,
		OnStart: func(_ string) {
			if s.Health != nil {
				s.Health.Up(health.ComponentNetconf)
			}
		},
		OnExit: func(_ string, err error, _ time.Duration) {
			if s.Health == nil {
				return
			}
			if err == nil || ctx.Err() != nil {
				return
			}
			downErr := emserrors.Wrap(err, emserrors.ErrCodeProcess, "netconf server crashed",
				emserrors.WithOp("netconf"),
				emserrors.WithSeverity(emserrors.SeverityCritical),
			)
			s.Health.Down(health.ComponentNetconf, downErr)
		},
	}
	return sup.Run(ctx)
}
