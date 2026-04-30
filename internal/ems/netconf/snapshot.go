package netconf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"lte-element-manager/internal/ems/domain/nrm"
	domainpm "lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/fcaps/alarms"
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
	Faults     *faultState   `json:"ems-fault-management:fault_management,omitempty"`
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

type faultState struct {
	ActiveAlarm []activeAlarm `json:"active_alarm,omitempty"`
}

type activeAlarm struct {
	AlarmID               string `json:"alarm_id"`
	ManagedObjectInstance string `json:"managed_object_instance,omitempty"`
	EventType             string `json:"event_type,omitempty"`
	ProbableCause         string `json:"probable_cause,omitempty"`
	PerceivedSeverity     string `json:"perceived_severity,omitempty"`
	SpecificProblem       string `json:"specific_problem,omitempty"`
	FirstEventTime        string `json:"first_event_time,omitempty"`
	LastEventTime         string `json:"last_event_time,omitempty"`
	OccurrenceCount       string `json:"occurrence_count,omitempty"`
}

func BuildCombinedSnapshot(cfg SnapshotConfig, reg *nrm.Registry, pmStore *pm.Store, normalizedLegacy string, alarmStores ...*alarms.Store) ([]byte, error) {
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
	if len(alarmStores) > 0 {
		fn.Faults = buildFaultState(alarmStores[0])
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

func buildFaultState(store *alarms.Store) *faultState {
	if store == nil {
		return nil
	}
	records := store.Active()
	if len(records) == 0 {
		return nil
	}
	out := &faultState{ActiveAlarm: make([]activeAlarm, 0, len(records))}
	for _, rec := range records {
		out.ActiveAlarm = append(out.ActiveAlarm, activeAlarm{
			AlarmID:               rec.AlarmID,
			ManagedObjectInstance: rec.ManagedObjectInstance,
			EventType:             rec.EventType,
			ProbableCause:         rec.ProbableCause,
			PerceivedSeverity:     rec.PerceivedSeverity,
			SpecificProblem:       rec.SpecificProblem,
			FirstEventTime:        rec.FirstSeen.UTC().Format(time.RFC3339Nano),
			LastEventTime:         rec.LastSeen.UTC().Format(time.RFC3339Nano),
			OccurrenceCount:       strconv.FormatUint(rec.Count, 10),
		})
	}
	return out
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
