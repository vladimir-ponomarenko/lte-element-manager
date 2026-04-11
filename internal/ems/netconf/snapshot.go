package netconf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"lte-element-manager/internal/ems/domain/nrm"
	domainpm "lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/fcaps/pm"
	emserrors "lte-element-manager/internal/errors"
)

type SnapshotConfig struct {
	SubNetwork     string
	ManagedElement string
	ENBFunctionID  string
}

type combinedSnapshot struct {
	Legacy     json.RawMessage `json:"ems-enb-metrics:enb_metrics,omitempty"`
	SubNetwork []subNetwork    `json:"_3gpp-common-managed-element:SubNetwork,omitempty"`
}

type subNetwork struct {
	ID             string           `json:"id"`
	ManagedElement []managedElement `json:"ManagedElement,omitempty"`
}

type managedElement struct {
	ID          string        `json:"id"`
	ENBFunction []enbFunction `json:"ENBFunction,omitempty"`
}

type enbFunction struct {
	ID         string        `json:"id"`
	EUtranCell []eUtranCell  `json:"EUtranCell,omitempty"`
	SRSRAN     *srsranVendor `json:"srsran-vendor-ext:srsran,omitempty"`
}

type eUtranCell struct {
	ID           string        `json:"id"`
	Measurements *measurements `json:"measurements,omitempty"`
}

type measurements struct {
	ThroughputDL string `json:"throughputDL,omitempty"`
	ThroughputUL string `json:"throughputUL,omitempty"`
	SINRUL       string `json:"sinrUL,omitempty"`
	CQIDL        string `json:"cqiDL,omitempty"`
}

type srsranVendor struct {
	EnbMetrics json.RawMessage `json:"enb_metrics,omitempty"`
}

func BuildCombinedSnapshot(cfg SnapshotConfig, reg *nrm.Registry, pmStore *pm.Store, normalizedLegacy string) ([]byte, error) {
	if cfg.SubNetwork == "" || cfg.ManagedElement == "" || cfg.ENBFunctionID == "" {
		return nil, emserrors.New(emserrors.ErrCodeConfig, "snapshot config is incomplete",
			emserrors.WithOp("netconf.snapshot"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	if reg == nil {
		return nil, emserrors.New(emserrors.ErrCodeInternal, "nrm registry is nil",
			emserrors.WithOp("netconf.snapshot"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	if normalizedLegacy == "" {
		normalizedLegacy = "{}"
	}
	legacy := json.RawMessage([]byte(normalizedLegacy))

	fn := enbFunction{
		ID:     cfg.ENBFunctionID,
		SRSRAN: &srsranVendor{EnbMetrics: legacy},
	}

	cells := reg.EUtranCells()
	if len(cells) > 0 {
		report, hasReport := latestReport(pmStore)
		fn.EUtranCell = make([]eUtranCell, 0, len(cells))
		for _, c := range cells {
			fn.EUtranCell = append(fn.EUtranCell, buildCell(c, report, hasReport))
		}
	}

	snap := combinedSnapshot{
		Legacy: legacy,
		SubNetwork: []subNetwork{
			{
				ID: cfg.SubNetwork,
				ManagedElement: []managedElement{
					{
						ID: cfg.ManagedElement,
						ENBFunction: []enbFunction{
							fn,
						},
					},
				},
			},
		},
	}

	out, err := json.Marshal(snap)
	if err != nil {
		return nil, emserrors.Wrap(err, emserrors.ErrCodeInternal, "build snapshot json failed",
			emserrors.WithOp("netconf.snapshot"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	return out, nil
}

func latestReport(store *pm.Store) (pm.Report, bool) {
	if store == nil {
		return pm.Report{}, false
	}
	return store.Latest()
}

func buildCell(c nrm.Object, report pm.Report, hasReport bool) eUtranCell {
	out := eUtranCell{ID: c.Name}
	if !hasReport {
		return out
	}
	mm := report.ByDN[c.DN]
	if len(mm) == 0 {
		return out
	}

	m := buildMeasurements(mm)
	if m != nil {
		out.Measurements = m
	}
	return out
}

func buildMeasurements(src map[string]pm.Value) *measurements {
	var out measurements
	seen := false
	for _, def := range domainpm.MeasurementDefinitions {
		v, ok := src[def.CanonicalKey]
		if !ok {
			continue
		}
		val := formatDecimal6(v.Value)
		switch def.Leaf {
		case domainpm.LeafThroughputDL:
			out.ThroughputDL = val
		case domainpm.LeafThroughputUL:
			out.ThroughputUL = val
		case domainpm.LeafSINRUL:
			out.SINRUL = val
		case domainpm.LeafCQIDL:
			out.CQIDL = val
		}
		seen = true
	}
	if !seen {
		return nil
	}
	return &out
}

func formatDecimal6(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func WriteSnapshotFile(path string, data []byte) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp", path)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
