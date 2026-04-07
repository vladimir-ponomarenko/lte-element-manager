package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/fcaps/alarms"
	"lte-element-manager/internal/ems/health"
	emserrors "lte-element-manager/internal/errors"
)

// FaultService ingests health signals and publishes alarm events on the bus.
type FaultService struct {
	Bus         *bus.Bus
	Health      *health.Tracker
	Manager     *alarms.Manager
	Log         zerolog.Logger
	MinSeverity emserrors.Severity
}

func NewFaultService(b *bus.Bus, h *health.Tracker, m *alarms.Manager, log zerolog.Logger) *FaultService {
	if m == nil {
		m = alarms.NewManager(nil)
	}
	return &FaultService{
		Bus:         b,
		Health:      h,
		Manager:     m,
		Log:         log,
		MinSeverity: emserrors.SeverityMajor,
	}
}

func (s *FaultService) Name() string { return "faults" }

func (s *FaultService) Run(ctx context.Context) error {
	if s.Health == nil || s.Bus == nil {
		return nil
	}

	sub := s.Health.Subscribe(ctx)
	for ev := range sub {
		switch ev.Type {
		case health.EventStateChange:
			s.Log.Info().
				Str("health", string(ev.State)).
				Str("prev_health", string(ev.PrevState)).
				Msg("health state changed")

		case health.EventComponentDown:
			s.onComponentDown(ev)

		case health.EventComponentUp:
			s.onComponentUp(ev)
		}
	}

	return nil
}

func (s *FaultService) onComponentDown(ev health.Event) {
	if ev.Err == nil {
		return
	}
	sev := emserrors.SeverityOf(ev.Err)
	if !emserrors.AtLeast(sev, s.MinSeverity) {
		s.Log.Debug().
			Str("component", string(ev.Component)).
			Str("severity", string(sev)).
			Err(ev.Err).
			Msg("health component down (below alarm threshold)")
		return
	}

	alarm := emserrors.Alarm(ev.Err)
	out, _ := s.Manager.Raise(ev.At, string(ev.Component), string(ev.State), alarm)

	s.Log.Error().
		Str("component", string(ev.Component)).
		Str("health", string(ev.State)).
		Str("alarm_code", out.Alarm.Code).
		Str("severity", out.Alarm.Severity).
		Uint64("count", out.Count).
		Msg(out.Alarm.Message)

	s.Bus.Publish(out)
}

func (s *FaultService) onComponentUp(ev health.Event) {
	cleared := s.Manager.ClearComponent(ev.At, string(ev.Component), string(ev.State))
	for _, out := range cleared {
		s.Log.Info().
			Str("component", string(ev.Component)).
			Str("health", string(ev.State)).
			Str("alarm_code", out.Alarm.Code).
			Str("severity", out.Alarm.Severity).
			Uint64("count", out.Count).
			Msg("alarm cleared")
		s.Bus.Publish(out)
	}
}
