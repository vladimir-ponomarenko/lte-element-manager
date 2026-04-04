package services

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/worker"
)

type NetconfServer struct {
	Worker worker.Worker
	Log    zerolog.Logger
}

func NewNetconfServer(w worker.Worker, log zerolog.Logger) *NetconfServer {
	return &NetconfServer{Worker: w, Log: log}
}

func (s *NetconfServer) Name() string {
	return "netconf_server"
}

func (s *NetconfServer) Run(ctx context.Context) error {
	sup := &worker.Supervisor{
		Worker:  s.Worker,
		Backoff: worker.NewExponentialBackoff(1*time.Second, 30*time.Second, 30*time.Second, 0.1),
		Log:     s.Log,
	}
	return sup.Run(ctx)
}
