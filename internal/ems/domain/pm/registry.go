package pm

import "lte-element-manager/internal/ems/domain/canonical"

const (
	CanonicalUEDLBitrate = "ue.dl_bitrate"
	CanonicalUEULBitrate = "ue.ul_bitrate"
	CanonicalUEULSNR     = "ue.ul_snr"
	CanonicalUEDLCQI     = "ue.dl_cqi"
)

const (
	LeafThroughputDL = "throughputDL"
	LeafThroughputUL = "throughputUL"
	LeafSINRUL       = "sinrUL"
	LeafCQIDL        = "cqiDL"
)

type MetricMeta struct {
	CanonicalKey string
	Leaf         string
	Type         canonical.MetricType
	Unit         string
	Description  string
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
}
