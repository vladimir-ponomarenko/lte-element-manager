package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/fcaps/alarms"
	pmfcaps "lte-element-manager/internal/ems/fcaps/pm"
	"lte-element-manager/internal/ems/fcaps/tca"
)

type TCAService struct {
	Bus     *bus.Bus
	Manager *alarms.Manager
	Engine  *tca.Engine
	Log     zerolog.Logger
}

func NewTCAService(b *bus.Bus, manager *alarms.Manager, cfg tca.Config, log zerolog.Logger) *TCAService {
	return &TCAService{
		Bus:     b,
		Manager: manager,
		Engine:  tca.NewEngine(manager, cfg, log),
		Log:     log,
	}
}

func (s *TCAService) Name() string { return "tca" }

func (s *TCAService) Run(ctx context.Context) error {
	if s.Bus == nil || s.Manager == nil || s.Engine == nil || !s.Engine.Enabled() {
		return nil
	}
	s.Log.Info().Msg("tca service enabled")
	sub := s.Bus.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-sub:
			if !ok {
				return nil
			}
			if ev, ok := msg.(pmfcaps.Event); ok {
				for _, alarmEvent := range s.Engine.Evaluate(ev.Report) {
					s.Bus.Publish(alarmEvent)
				}
			}
		}
	}
}
