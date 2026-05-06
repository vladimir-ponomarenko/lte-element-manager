package services

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/netconfcm"
)

type NetconfSessionGC struct {
	Manager  *netconfcm.Manager
	Interval time.Duration
	Log      zerolog.Logger
}

func NewNetconfSessionGC(manager *netconfcm.Manager, log zerolog.Logger) *NetconfSessionGC {
	return &NetconfSessionGC{Manager: manager, Interval: 15 * time.Second, Log: log}
}

func (s *NetconfSessionGC) Name() string { return "netconf_session_gc" }

func (s *NetconfSessionGC) Run(ctx context.Context) error {
	if s.Manager == nil {
		return nil
	}
	interval := s.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			expired := s.Manager.SweepExpiredSessions(now)
			if len(expired) > 0 {
				s.Log.Warn().Uints64("session_ids", expired).Msg("stale NETCONF sessions garbage collected")
			}
		}
	}
}
