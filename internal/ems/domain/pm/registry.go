package pm

import (
	"strings"

	"lte-element-manager/internal/ems/domain/canonical"
)

const (
	CanonicalUEDLBitrate     = "ue.dl_bitrate"
	CanonicalUEULBitrate     = "ue.ul_bitrate"
	CanonicalUEULSNR         = "ue.ul_snr"
	CanonicalUEDLCQI         = "ue.dl_cqi"
	CanonicalUEDLBLER        = "ue.dl_bler"
	CanonicalUEULBLER        = "ue.ul_bler"
	CanonicalUEULPUCCHNI     = "ue.ul_pucch_ni"
	CanonicalUEULPHR         = "ue.ul_phr"
	CanonicalUERRCRLFCnt     = "ue.rrc_rlf_cnt"
	CanonicalUERRCInactivity = "ue.rrc_release_cause_inactivity"
	CanonicalRRCConnectedUES = "rrc.rrc_connected_ues"
	CanonicalS1APReady       = "s1ap.ready"
	CanonicalS1APStatusCode  = "s1ap.status_code"
	CanonicalNASDLDrop       = "s1ap.nas_dl_drop"
	CanonicalNASULFail       = "s1ap.nas_ul_fail"
	CanonicalNASULSecUnknown = "s1ap.nas_ul_sec_hdr_unknown"
	CanonicalNASULParseFail  = "s1ap.nas_ul_parse_fail"
	CanonicalNASDLParseFail  = "s1ap.nas_dl_parse_fail"
	CanonicalNASDLServiceRej = "s1ap.nas_dl_service_reject"
	CanonicalRRCProtocolFail = "rrc.rrc_protocol_fail"
	CanonicalRRCConRejectTX  = "rrc.rrc_con_reject_tx"
	CanonicalRRCPagingFail   = "rrc.rrc_paging_add_fail"
	CanonicalRRCMaxRLCRetx   = "rrc.rrc_max_rlc_retx"
)

const (
	LeafThroughputDL = "throughputDL"
	LeafThroughputUL = "throughputUL"
	LeafSINRUL       = "sinrUL"
	LeafCQIDL        = "cqiDL"
	LeafBLERDL       = "blerDL"
	LeafBLERUL       = "blerUL"
	LeafRRCConnected = "rrcConnectedUEs"
)

type MetricMeta struct {
	CanonicalKey string
	Leaf         string
	Type         canonical.MetricType
	Unit         string
	Description  string
	Pattern      bool
}

var MeasurementDefinitions = []MetricMeta{
	{
		CanonicalKey: CanonicalUEDLBitrate,
		Leaf:         LeafThroughputDL,
		Type:         canonical.Gauge,
		Unit:         "bps",
		Description:  "Downlink throughput (UE sample gauge).",
	},
	{
		CanonicalKey: CanonicalUEULBitrate,
		Leaf:         LeafThroughputUL,
		Type:         canonical.Gauge,
		Unit:         "bps",
		Description:  "Uplink throughput (UE sample gauge).",
	},
	{
		CanonicalKey: CanonicalUEULSNR,
		Leaf:         LeafSINRUL,
		Type:         canonical.Gauge,
		Unit:         "dB",
		Description:  "Uplink SINR (UE sample gauge).",
	},
	{
		CanonicalKey: CanonicalUEDLCQI,
		Leaf:         LeafCQIDL,
		Type:         canonical.Gauge,
		Unit:         "index",
		Description:  "Downlink CQI (UE sample gauge).",
	},
	{
		CanonicalKey: CanonicalUEDLBLER,
		Leaf:         LeafBLERDL,
		Type:         canonical.Gauge,
		Unit:         "ratio",
		Description:  "Downlink BLER (UE sample gauge).",
	},
	{
		CanonicalKey: CanonicalUEULBLER,
		Leaf:         LeafBLERUL,
		Type:         canonical.Gauge,
		Unit:         "ratio",
		Description:  "Uplink BLER (UE sample gauge).",
	},
	{
		CanonicalKey: CanonicalRRCConnectedUES,
		Leaf:         LeafRRCConnected,
		Type:         canonical.Gauge,
		Unit:         "count",
		Description:  "Currently connected RRC UEs.",
	},
	{CanonicalKey: CanonicalS1APReady, Type: canonical.Gauge, Unit: "boolean", Description: "S1AP ready state encoded as 1/0."},
	{CanonicalKey: CanonicalS1APStatusCode, Type: canonical.Gauge, Unit: "count", Description: "S1AP status code."},
	{CanonicalKey: CanonicalNASDLDrop, Type: canonical.Counter, Unit: "count", Description: "Dropped downlink NAS messages."},
	{CanonicalKey: CanonicalNASULFail, Type: canonical.Counter, Unit: "count", Description: "Failed uplink NAS messages."},
	{CanonicalKey: CanonicalNASULSecUnknown, Type: canonical.Counter, Unit: "count", Description: "Unknown uplink NAS security headers."},
	{CanonicalKey: CanonicalNASULParseFail, Type: canonical.Counter, Unit: "count", Description: "Uplink NAS parse failures."},
	{CanonicalKey: CanonicalNASDLParseFail, Type: canonical.Counter, Unit: "count", Description: "Downlink NAS parse failures."},
	{CanonicalKey: CanonicalNASDLServiceRej, Type: canonical.Counter, Unit: "count", Description: "Downlink NAS service rejects."},
	{CanonicalKey: CanonicalRRCProtocolFail, Type: canonical.Counter, Unit: "count", Description: "RRC protocol failures."},
	{CanonicalKey: CanonicalRRCConRejectTX, Type: canonical.Counter, Unit: "count", Description: "RRC connection reject transmissions."},
	{CanonicalKey: CanonicalRRCPagingFail, Type: canonical.Counter, Unit: "count", Description: "Paging add failures."},
	{CanonicalKey: CanonicalRRCMaxRLCRetx, Type: canonical.Counter, Unit: "count", Description: "RRC max RLC retransmission events."},
	{CanonicalKey: CanonicalUEULPUCCHNI, Type: canonical.Gauge, Unit: "dBm", Description: "Uplink PUCCH noise and interference."},
	{CanonicalKey: CanonicalUEULPHR, Type: canonical.Gauge, Unit: "dB", Description: "UE uplink power headroom."},
	{CanonicalKey: CanonicalUERRCRLFCnt, Type: canonical.Counter, Unit: "count", Description: "UE radio link failure counter."},
	{CanonicalKey: CanonicalUERRCInactivity, Type: canonical.Gauge, Unit: "boolean", Description: "UE release cause is inactivity."},
	{CanonicalKey: "bearer.*.dl_buffered_bytes", Type: canonical.Gauge, Unit: "bytes", Description: "Downlink buffered bytes per bearer.", Pattern: true},
}

// IsAllowedCanonicalKey reports whether a canonical metric participates in PM.
func IsAllowedCanonicalKey(name string) bool {
	for _, def := range MeasurementDefinitions {
		if def.Pattern {
			if wildcardMatch(def.CanonicalKey, name) {
				return true
			}
			continue
		}
		if def.CanonicalKey == name {
			return true
		}
	}
	return false
}

func wildcardMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	pos := 0
	if parts[0] != "" {
		if !strings.HasPrefix(value, parts[0]) {
			return false
		}
		pos = len(parts[0])
	}
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(value, last)
}
