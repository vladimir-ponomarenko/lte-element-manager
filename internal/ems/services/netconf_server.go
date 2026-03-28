package services

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type runner interface {
	Run(ctx context.Context) error
}

type NetconfServer struct {
	Server runner
	Log    zerolog.Logger
}

func NewNetconfServer(server runner, log zerolog.Logger) *NetconfServer {
	return &NetconfServer{Server: server, Log: log}
}

func (s *NetconfServer) Name() string {
	return "netconf_server"
}

func (s *NetconfServer) Run(ctx context.Context) error {
	backoff := 2 * time.Second
	for {
		if err := s.Server.Run(ctx); err != nil {
			s.Log.Error().Err(err).Msg("netconf server crashed")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			continue
		}
		return nil
	}
}
