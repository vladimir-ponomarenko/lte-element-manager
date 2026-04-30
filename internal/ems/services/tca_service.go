package services

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/fcaps/alarms"
	pmfcaps "lte-element-manager/internal/ems/fcaps/pm"
)

type TCAService struct {
	Bus      *bus.Bus
	Manager  *alarms.Manager
	Log      zerolog.Logger
	Duration time.Duration

	lowSince map[string]time.Time
}

func NewTCAService(b *bus.Bus, manager *alarms.Manager, log zerolog.Logger) *TCAService {
	return &TCAService{
		Bus:      b,
		Manager:  manager,
		Log:      log,
		Duration: 3 * time.Minute,
		lowSince: make(map[string]time.Time),
	}
}

func (s *TCAService) Name() string { return "tca" }

func (s *TCAService) Run(ctx context.Context) error {
	if s.Bus == nil || s.Manager == nil {
		return nil
	}
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
				s.evaluateReport(ev.Report)
			}
		}
	}
}

func (s *TCAService) evaluateReport(report pmfcaps.Report) {
	now := report.End
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for dn, values := range report.ByDN {
		dl, hasDL := values[pm.CanonicalUEDLBitrate]
		ul, hasUL := values[pm.CanonicalUEULBitrate]
		if !hasDL && !hasUL {
			continue
		}
		total := dl.Value + ul.Value
		key := string(dn)
		if total > 0 {
			delete(s.lowSince, key)
			continue
		}
		start, ok := s.lowSince[key]
		if !ok {
			s.lowSince[key] = now
			continue
		}
		if now.Sub(start) < s.Duration {
			continue
		}
		alarm := alarms.NewThresholdAlarm(alarms.AlarmLowThroughput, key, "DL/UL throughput remained zero", total)
		evt, changed := s.Manager.Raise(now, "pm", "degraded", alarm)
		if changed && s.Bus != nil {
			s.Bus.Publish(evt)
		}
	}
}
