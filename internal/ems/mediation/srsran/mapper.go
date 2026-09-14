package srsran

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/domain/canonical"
	domainpm "lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/mediation"
	emserrors "lte-element-manager/internal/errors"
)

type Mapper struct {
	SourceID string
}

func (m *Mapper) Map(rawJSON string) ([]canonical.Sample, error) {
	if rawJSON == "" {
		return nil, emserrors.New(emserrors.ErrCodeDataCorrupt, "empty metrics payload",
			emserrors.WithOp("mediation.srsran"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &envelope); err != nil {
		return nil, fmt.Errorf("invalid metrics json: %w", err)
	}
	if envelope.Type == "gnb_metrics" {
		return m.mapGNB(rawJSON)
	}
	parsed, err := parseEnbMetrics([]byte(rawJSON))
	if err != nil {
		return nil, err
	}

	tsMillis := int64(parsed.Timestamp * 1000)
	sourceID := m.SourceID
	if sourceID == "" {
		sourceID = parsed.EnbSerial
	}

	out := make([]canonical.Sample, 0, 1+len(parsed.CellList))
	out = append(out, mapNode(tsMillis, sourceID, parsed))
	out = append(out, mapCells(tsMillis, sourceID, parsed.CellList)...)
	return out, nil
}

func (m *Mapper) mapGNB(rawJSON string) ([]canonical.Sample, error) {
	var g domain.GnbMetrics
	if err := json.Unmarshal([]byte(rawJSON), &g); err != nil {
		return nil, err
	}
	if g.Type != "gnb_metrics" || g.Timestamp == 0 || g.Identity() == "" {
		return nil, emserrors.New(emserrors.ErrCodeDataCorrupt, "invalid gNB metrics envelope", emserrors.WithOp("mediation.srsran"), emserrors.WithSeverity(emserrors.SeverityMajor))
	}
	source := m.SourceID
	if source == "" {
		source = g.Identity()
	}
	ts := int64(g.Timestamp * 1000)
	node := canonical.Sample{Timestamp: ts, SourceID: source, Scope: "node", Metrics: map[string]canonical.Metric{}, Attrs: map[string]string{"type": g.Type, "gnb_id": g.Identity()}}
	mapNRCounters(node.Metrics, "ngap", g.NGAP)
	mapNRCounters(node.Metrics, "rrc", g.RRC)
	if ready, ok := nrReady(g.NGAP); ok {
		putGauge(node.Metrics, domainpm.CanonicalNGAPReady, "boolean", ready)
	}
	out := []canonical.Sample{node}
	for _, c := range g.CellList {
		cellID := c.Identity()
		cell := canonical.Sample{Timestamp: ts, SourceID: source, Scope: "cell:nr=" + cellID, Metrics: map[string]canonical.Metric{}, Attrs: map[string]string{"nci": string(c.NCI), "nr_cell_id": string(c.NRCellID), "pci": strconv.FormatUint(uint64(c.PCI), 10)}}
		out = append(out, cell)
		for _, u := range c.UEList {
			ue := canonical.Sample{Timestamp: ts, SourceID: source, Scope: cell.Scope + "/ue:rnti=" + strconv.FormatUint(uint64(u.RNTI), 10), Metrics: map[string]canonical.Metric{}, Attrs: map[string]string{"ue_rnti": strconv.FormatUint(uint64(u.RNTI), 10)}}
			putGauge(ue.Metrics, domainpm.CanonicalUEDLCQI, "index", u.DLCQI)
			putGauge(ue.Metrics, domainpm.CanonicalUEDLBitrate, "bps", u.DLBitrate)
			putGauge(ue.Metrics, domainpm.CanonicalUEDLBLER, "ratio", u.DLBLER)
			putGauge(ue.Metrics, domainpm.CanonicalUEULSNR, "dB", u.ULSINR)
			putGauge(ue.Metrics, domainpm.CanonicalUEULBitrate, "bps", u.ULBitrate)
			putGauge(ue.Metrics, domainpm.CanonicalUEULBLER, "ratio", u.ULBLER)
			putGauge(ue.Metrics, domainpm.CanonicalUEULPHR, "dB", u.ULPHR)
			out = append(out, ue)
		}
	}
	return out, nil
}

func mapNRCounters(dst map[string]canonical.Metric, prefix string, values map[string]any) {
	for k, raw := range values {
		if v, ok := raw.(float64); ok {
			if prefix == "rrc" && k == "rrc_connected_ues" {
				putGauge(dst, domainpm.CanonicalRRCConnectedUES, "count", v)
				continue
			}
			putCounter(dst, prefix+"."+k, "count", v)
		}
	}
}
func nrReady(values map[string]any) (float64, bool) {
	for _, k := range []string{"ngap_status", "status"} {
		if s, ok := values[k].(string); ok {
			if strings.EqualFold(s, "ready") || strings.EqualFold(s, "connected") {
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

func parseEnbMetrics(raw []byte) (*domain.EnbMetrics, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var m domain.EnbMetrics
	if err := dec.Decode(&m); err != nil {
		return nil, emserrors.New(emserrors.ErrCodeDataCorrupt, "invalid metrics json",
			emserrors.WithOp("mediation.srsran"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}
	if m.Type != "enb_metrics" {
		return nil, emserrors.New(emserrors.ErrCodeDataCorrupt, "unexpected metrics type",
			emserrors.WithOp("mediation.srsran"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}
	if m.EnbSerial == "" || m.Timestamp == 0 {
		return nil, emserrors.New(emserrors.ErrCodeDataCorrupt, "missing required root fields",
			emserrors.WithOp("mediation.srsran"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}
	return &m, nil
}

func mapNode(tsMillis int64, sourceID string, m *domain.EnbMetrics) canonical.Sample {
	s := canonical.Sample{
		Timestamp: tsMillis,
		SourceID:  sourceID,
		Scope:     "node",
		Metrics:   make(map[string]canonical.Metric, 2+len(m.S1AP.Counters)+len(m.RRC.Counters)),
		Attrs: map[string]string{
			"type":        m.Type,
			"enb_serial":  m.EnbSerial,
			"s1ap_status": m.S1AP.Status,
		},
	}

	ready := 0.0
	if strings.EqualFold(strings.TrimSpace(m.S1AP.Status), "ready") || m.S1AP.StatusCode == 1 {
		ready = 1
	}
	putGauge(s.Metrics, domainpm.CanonicalS1APReady, "boolean", ready)
	putGauge(s.Metrics, KeyS1APStatusCode, "count", float64(m.S1AP.StatusCode))

	for k, v := range m.S1AP.Counters {
		putCounter(s.Metrics, "s1ap."+k, "count", float64(v))
	}
	for k, v := range m.RRC.Counters {
		if k == "rrc_connected_ues" {
			putGauge(s.Metrics, KeyRRCConnectedUES, "count", float64(v))
			continue
		}
		putCounter(s.Metrics, "rrc."+k, "count", float64(v))
	}

	return s
}

func mapCells(tsMillis int64, sourceID string, cells []domain.CellContainer) []canonical.Sample {
	out := make([]canonical.Sample, 0, len(cells))
	for _, c := range cells {
		cellID := fmt.Sprintf("cell:carrier=%d,pci=%d", c.CarrierID, c.PCI)
		s := canonical.Sample{
			Timestamp: tsMillis,
			SourceID:  sourceID,
			Scope:     cellID,
			Metrics:   make(map[string]canonical.Metric, 8),
			Attrs: map[string]string{
				"carrier_id": strconv.FormatUint(uint64(c.CarrierID), 10),
				"pci":        strconv.FormatUint(uint64(c.PCI), 10),
			},
		}

		putCounter(s.Metrics, KeyCellNoFRACH, "count", float64(c.NoFRACH))
		out = append(out, s)

		for _, ue := range c.UEList {
			out = append(out, mapUE(tsMillis, sourceID, cellID, ue))
		}
	}
	return out
}

func mapUE(tsMillis int64, sourceID, cellID string, u domain.UEContainer) canonical.Sample {
	scope := fmt.Sprintf("%s/ue:rnti=%d", cellID, u.RNTI)
	s := canonical.Sample{
		Timestamp: tsMillis,
		SourceID:  sourceID,
		Scope:     scope,
		Metrics:   make(map[string]canonical.Metric, 64),
		Attrs: map[string]string{
			"ue_rnti":           strconv.FormatUint(uint64(u.RNTI), 10),
			"rrc_state_str":     u.RRCStateStr,
			"rrc_release_cause": u.RRCReleaseCause,
		},
	}

	mediation.ApplyFieldRules(s.Metrics, u, UEFieldRules)
	inactivityRelease := 0.0
	if strings.EqualFold(strings.TrimSpace(u.RRCReleaseCause), "inactivity") {
		inactivityRelease = 1
	}
	putGauge(s.Metrics, domainpm.CanonicalUERRCInactivity, "boolean", inactivityRelease)

	for _, b := range u.BearerList {
		m := make(map[string]canonical.Metric, 8)
		mediation.ApplyFieldRules(m, b, BearerFieldRules)
		for k, v := range m {
			v.Name = "bearer." + k
			s.Metrics[fmt.Sprintf("bearer.%d.%s", b.BearerID, k)] = v
		}
		s.Attrs[fmt.Sprintf("bearer.%d.qci", b.BearerID)] = strconv.FormatUint(uint64(b.QCI), 10)
	}

	return s
}

func putCounter(dst map[string]canonical.Metric, name, unit string, v float64) {
	dst[name] = canonical.Metric{Name: name, Type: canonical.Counter, Value: v, Unit: unit}
}

func putGauge(dst map[string]canonical.Metric, name, unit string, v float64) {
	dst[name] = canonical.Metric{Name: name, Type: canonical.Gauge, Value: v, Unit: unit}
}
