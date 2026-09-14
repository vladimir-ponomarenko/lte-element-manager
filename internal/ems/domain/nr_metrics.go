package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

// StringID accepts both string and numeric identifiers without float64 loss.
type StringID string

func (v *StringID) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		*v = StringID(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	*v = StringID(n.String())
	return nil
}

// GnbMetrics is the small, stable NR telemetry envelope accepted by FCAPS.
// It intentionally leaves protocol counters as maps: srsRAN versions expose
// different counter sets, while the known counters are normalized by mediation.
type GnbMetrics struct {
	Type      string            `json:"type"`
	Timestamp float64           `json:"timestamp"`
	GnbID     StringID          `json:"gnb_id"`
	GnbSerial StringID          `json:"gnb_serial"`
	NGAP      map[string]any    `json:"ngap_container"`
	RRC       map[string]any    `json:"rrc_container"`
	CellList  []NRCellContainer `json:"cell_list"`
}

func (m GnbMetrics) Identity() string {
	if strings.TrimSpace(string(m.GnbSerial)) != "" {
		return string(m.GnbSerial)
	}
	return string(m.GnbID)
}

type NRCellContainer struct {
	NCI      StringID        `json:"nci"`
	NRCellID StringID        `json:"nr_cell_id"`
	PCI      uint32          `json:"pci"`
	UEList   []NRUEContainer `json:"ue_list"`
}

func (c NRCellContainer) Identity() string {
	if c.NCI != "" {
		return string(c.NCI)
	}
	if c.NRCellID != "" {
		return string(c.NRCellID)
	}
	return "pci=" + strconv.FormatUint(uint64(c.PCI), 10)
}

// UnmarshalJSON also accepts the wrapped cell form used by some exporters.
func (c *NRCellContainer) UnmarshalJSON(raw []byte) error {
	type plain NRCellContainer
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return err
	}
	if inner, ok := outer["cell_container"]; ok {
		return json.Unmarshal(inner, (*plain)(c))
	}
	return json.Unmarshal(raw, (*plain)(c))
}

// NRUEContainer contains the common radio KPIs. Unknown fields can still be
// added to the source without breaking parsing.
type NRUEContainer struct {
	RNTI      uint32  `json:"ue_rnti"`
	DLCQI     float64 `json:"dl_cqi"`
	DLBitrate float64 `json:"dl_bitrate"`
	DLBLER    float64 `json:"dl_bler"`
	ULSINR    float64 `json:"ul_sinr"`
	ULBitrate float64 `json:"ul_bitrate"`
	ULBLER    float64 `json:"ul_bler"`
	ULPHR     float64 `json:"ul_phr"`
}
