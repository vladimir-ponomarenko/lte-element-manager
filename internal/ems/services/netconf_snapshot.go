package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/domain/nrm"
	"lte-element-manager/internal/ems/fcaps/alarms"
	"lte-element-manager/internal/ems/fcaps/metrics"
	"lte-element-manager/internal/ems/fcaps/pm"
	mediationSRSRAN "lte-element-manager/internal/ems/mediation/srsran"
	"lte-element-manager/internal/ems/netconf"
)

type NetconfSnapshot struct {
	In      <-chan domain.MetricSample
	Bus     *bus.Bus
	Legacy  *metrics.Store
	Path    string
	NRM     netconf.SnapshotConfig
	Reg     *nrm.Registry
	PMStore *pm.Store
	Alarms  *alarms.Store
	Log     zerolog.Logger
}

func NewNetconfSnapshot(in <-chan domain.MetricSample, b *bus.Bus, legacy *metrics.Store, path string, nrmCfg netconf.SnapshotConfig, reg *nrm.Registry, pmStore *pm.Store, alarmStore *alarms.Store, log zerolog.Logger) *NetconfSnapshot {
	return &NetconfSnapshot{
		In:      in,
		Bus:     b,
		Legacy:  legacy,
		Path:    path,
		NRM:     nrmCfg,
		Reg:     reg,
		PMStore: pmStore,
		Alarms:  alarmStore,
		Log:     log,
	}
}

func (s *NetconfSnapshot) Name() string { return "netconf_snapshot" }

func (s *NetconfSnapshot) Run(ctx context.Context) error {
	if s.In == nil {
		return nil
	}

	lastNormalized := "{}"
	if s.Path != "" {
		b, err := netconf.BuildCombinedSnapshot(s.NRM, s.Reg, s.PMStore, lastNormalized, s.Alarms)
		if err != nil {
			s.Log.Warn().Err(err).Msg("netconf snapshot init build failed")
		} else if err := netconf.WriteSnapshotFile(s.Path, b); err != nil {
			s.Log.Warn().Err(err).Msg("netconf snapshot init write failed")
		}
	}
	var busEvents <-chan bus.Message
	if s.Bus != nil {
		busEvents = s.Bus.Subscribe(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-busEvents:
			if !ok {
				busEvents = nil
				continue
			}
			switch msg.(type) {
			case alarms.Event, pm.Event:
				s.writeSnapshot(lastNormalized)
			}
		case sample, ok := <-s.In:
			if !ok {
				return nil
			}
			normalized, err := mediationSRSRAN.NormalizeForNetconf(sample.RawJSON)
			if err != nil {
				s.Log.Warn().Err(err).Msg("metrics snapshot normalize failed")
				continue
			}

			if s.Legacy != nil {
				s.Legacy.Update(domain.MetricSample{RawJSON: normalized})
			}
			if s.Path == "" {
				continue
			}
			lastNormalized = normalized

			s.writeSnapshot(lastNormalized)
		}
	}
}

func (s *NetconfSnapshot) writeSnapshot(normalized string) {
	if s.Path == "" {
		return
	}
	b, err := netconf.BuildCombinedSnapshot(s.NRM, s.Reg, s.PMStore, normalized, s.Alarms)
	if err != nil {
		s.Log.Warn().Err(err).Msg("netconf snapshot build failed")
		return
	}
	if err := netconf.WriteSnapshotFile(s.Path, b); err != nil {
		s.Log.Warn().Err(err).Msg("netconf snapshot write failed")
	}
}
