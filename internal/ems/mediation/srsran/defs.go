package srsran

import (
	"lte-element-manager/internal/ems/domain/canonical"
	"lte-element-manager/internal/ems/mediation"
)

const (
	KeyS1APStatusCode = "s1ap.status_code"

	KeyCellNoFRACH = "cell.nof_rach"

	KeyUEDLCQI       = "ue.dl_cqi"
	KeyUEDLMCS       = "ue.dl_mcs"
	KeyUEDLBitrate   = "ue.dl_bitrate"
	KeyUEDLBLER      = "ue.dl_bler"
	KeyUEULSNR       = "ue.ul_snr"
	KeyUEULMCS       = "ue.ul_mcs"
	KeyUEULBitrate   = "ue.ul_bitrate"
	KeyUEULBLER      = "ue.ul_bler"
	KeyUEULPUSCHRSSI = "ue.ul_pusch_rssi"
	KeyUEULPUCCHRSSI = "ue.ul_pucch_rssi"
	KeyUEULPUCCHNI   = "ue.ul_pucch_ni"
	KeyUEDLCQIOffset = "ue.dl_cqi_offset"
	KeyUEULSNROffset = "ue.ul_snr_offset"
	KeyUEULPHR       = "ue.ul_phr"
	KeyUEULBSR       = "ue.ul_bsr"

	KeyUERRCState    = "ue.rrc_state"
	KeyUERRCDRBCount = "ue.rrc_drb_count"
	KeyUERRCNoFCells = "ue.rrc_nof_cells"
)

var UEFieldRules = []mediation.FieldRule{
	{JSONTag: "dl_cqi", Key: KeyUEDLCQI, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "dl_mcs", Key: KeyUEDLMCS, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "dl_bitrate", Key: KeyUEDLBitrate, Type: canonical.Gauge, Unit: "bps"},
	{JSONTag: "dl_bler", Key: KeyUEDLBLER, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "ul_snr", Key: KeyUEULSNR, Type: canonical.Gauge, Unit: "dB"},
	{JSONTag: "ul_mcs", Key: KeyUEULMCS, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "ul_bitrate", Key: KeyUEULBitrate, Type: canonical.Gauge, Unit: "bps"},
	{JSONTag: "ul_bler", Key: KeyUEULBLER, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "ul_pusch_rssi", Key: KeyUEULPUSCHRSSI, Type: canonical.Gauge, Unit: "dBm"},
	{JSONTag: "ul_pucch_rssi", Key: KeyUEULPUCCHRSSI, Type: canonical.Gauge, Unit: "dBm"},
	{JSONTag: "ul_pucch_ni", Key: KeyUEULPUCCHNI, Type: canonical.Gauge, Unit: "dBm"},
	{JSONTag: "dl_cqi_offset", Key: KeyUEDLCQIOffset, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "ul_snr_offset", Key: KeyUEULSNROffset, Type: canonical.Gauge, Unit: ""},
	{JSONTag: "ul_phr", Key: KeyUEULPHR, Type: canonical.Gauge, Unit: "dB"},
	{JSONTag: "ul_bsr", Key: KeyUEULBSR, Type: canonical.Gauge, Unit: "bytes"},
	{JSONTag: "rrc_state", Key: KeyUERRCState, Type: canonical.Gauge, Unit: "count"},
	{JSONTag: "rrc_drb_count", Key: KeyUERRCDRBCount, Type: canonical.Gauge, Unit: "count"},
	{JSONTag: "rrc_nof_cells", Key: KeyUERRCNoFCells, Type: canonical.Gauge, Unit: "count"},
}

var BearerFieldRules = []mediation.FieldRule{
	{JSONTag: "dl_total_bytes", Key: "dl_total_bytes", Type: canonical.Counter, Unit: "bytes"},
	{JSONTag: "ul_total_bytes", Key: "ul_total_bytes", Type: canonical.Counter, Unit: "bytes"},
	{JSONTag: "dl_latency", Key: "dl_latency", Type: canonical.Gauge, Unit: ""},
	{JSONTag: "ul_latency", Key: "ul_latency", Type: canonical.Gauge, Unit: ""},
	{JSONTag: "dl_buffered_bytes", Key: "dl_buffered_bytes", Type: canonical.Gauge, Unit: "bytes"},
	{JSONTag: "ul_buffered_bytes", Key: "ul_buffered_bytes", Type: canonical.Gauge, Unit: "bytes"},
}
