package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/domain/nrm"
	"lte-element-manager/internal/ems/fcaps/metrics"
	"lte-element-manager/internal/ems/fcaps/pm"
	mediationSRSRAN "lte-element-manager/internal/ems/mediation/srsran"
	"lte-element-manager/internal/ems/netconf"
)

type NetconfSnapshot struct {
	In      <-chan domain.MetricSample
	Legacy  *metrics.Store
	Path    string
	NRM     netconf.SnapshotConfig
	Reg     *nrm.Registry
	PMStore *pm.Store
	Log     zerolog.Logger
}

func NewNetconfSnapshot(in <-chan domain.MetricSample, legacy *metrics.Store, path string, nrmCfg netconf.SnapshotConfig, reg *nrm.Registry, pmStore *pm.Store, log zerolog.Logger) *NetconfSnapshot {
	return &NetconfSnapshot{
		In:      in,
		Legacy:  legacy,
		Path:    path,
		NRM:     nrmCfg,
		Reg:     reg,
		PMStore: pmStore,
		Log:     log,
	}
}

func (s *NetconfSnapshot) Name() string { return "netconf_snapshot" }

func (s *NetconfSnapshot) Run(ctx context.Context) error {
	if s.In == nil {
		return nil
	}

	if s.Path != "" {
		b, err := netconf.BuildCombinedSnapshot(s.NRM, s.Reg, s.PMStore, "{}")
		if err != nil {
			s.Log.Warn().Err(err).Msg("netconf snapshot init build failed")
		} else if err := netconf.WriteSnapshotFile(s.Path, b); err != nil {
			s.Log.Warn().Err(err).Msg("netconf snapshot init write failed")
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
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

			b, err := netconf.BuildCombinedSnapshot(s.NRM, s.Reg, s.PMStore, normalized)
			if err != nil {
				s.Log.Warn().Err(err).Msg("netconf snapshot build failed")
				continue
			}
			if err := netconf.WriteSnapshotFile(s.Path, b); err != nil {
				s.Log.Warn().Err(err).Msg("netconf snapshot write failed")
				continue
			}
		}
	}
}
