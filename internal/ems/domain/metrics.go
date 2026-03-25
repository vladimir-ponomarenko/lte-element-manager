package domain

import (
	"encoding/json"
	"fmt"
)

// EnbMetrics is the typed representation of srsENB JSON payload.
type EnbMetrics struct {
	Type      string          `json:"type"`
	EnbSerial string          `json:"enb_serial"`
	Timestamp float64         `json:"timestamp"`
	S1AP      S1APContainer   `json:"s1ap_container"`
	RRC       RRCContainer    `json:"rrc_container"`
	CellList  []CellContainer `json:"cell_list"`
}

type S1APContainer struct {
	Status     string            `json:"s1ap_status"`
	StatusCode int               `json:"s1ap_status_code"`
	Counters   map[string]uint64 `json:"-"`
}

func (c *S1APContainer) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Counters = make(map[string]uint64, len(raw))
	for k, v := range raw {
		switch k {
		case "s1ap_status":
			if err := json.Unmarshal(v, &c.Status); err != nil {
				return err
			}
		case "s1ap_status_code":
			if err := json.Unmarshal(v, &c.StatusCode); err != nil {
				return err
			}
		default:
			n, err := parseUint64(v)
			if err != nil {
				return fmt.Errorf("s1ap.%s: %w", k, err)
			}
			c.Counters[k] = n
		}
	}
	return nil
}

type RRCContainer struct {
	Counters map[string]uint64 `json:"-"`
}

func (c *RRCContainer) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Counters = make(map[string]uint64, len(raw))
	for k, v := range raw {
		n, err := parseUint64(v)
		if err != nil {
			return fmt.Errorf("rrc.%s: %w", k, err)
		}
		c.Counters[k] = n
	}
	return nil
}

type CellContainer struct {
	CarrierID uint32        `json:"carrier_id"`
	PCI       uint32        `json:"pci"`
	NoFRACH   uint32        `json:"nof_rach"`
	UEList    []UEContainer `json:"ue_list"`
}

func (c *CellContainer) UnmarshalJSON(data []byte) error {
	type alias CellContainer
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if inner, ok := raw["cell_container"]; ok {
		return json.Unmarshal(inner, (*alias)(c))
	}
	return json.Unmarshal(data, (*alias)(c))
}

type UEContainer struct {
	RNTI                     uint16            `json:"ue_rnti"`
	DLCQI                    float64           `json:"dl_cqi"`
	DLMCS                    float64           `json:"dl_mcs"`
	DLBitrate                float64           `json:"dl_bitrate"`
	DLBLER                   float64           `json:"dl_bler"`
	ULSNR                    float64           `json:"ul_snr"`
	ULMCS                    float64           `json:"ul_mcs"`
	ULPUSCHRSSI              float64           `json:"ul_pusch_rssi"`
	ULPUCCHRSSI              float64           `json:"ul_pucch_rssi"`
	ULPUCCHNI                float64           `json:"ul_pucch_ni"`
	ULPUSCHTPC               int64             `json:"ul_pusch_tpc"`
	ULPUCCHTPC               int64             `json:"ul_pucch_tpc"`
	DLCQIOffset              float64           `json:"dl_cqi_offset"`
	ULSNROffset              float64           `json:"ul_snr_offset"`
	ULBitrate                float64           `json:"ul_bitrate"`
	ULBLER                   float64           `json:"ul_bler"`
	ULPHR                    float64           `json:"ul_phr"`
	ULBSR                    uint32            `json:"ul_bsr"`
	RRCStateStr              string            `json:"rrc_state_str"`
	RRCState                 uint32            `json:"rrc_state"`
	RRCDRBCount              uint32            `json:"rrc_drb_count"`
	RRCNoFCells              uint32            `json:"rrc_nof_cells"`
	RRCIsAllocated           uint32            `json:"rrc_is_allocated"`
	RRCSRResPresent          uint32            `json:"rrc_sr_res_present"`
	RRCNPucchCSPresent       uint32            `json:"rrc_n_pucch_cs_present"`
	RRCIsCSFB                uint32            `json:"rrc_is_csfb"`
	RRCConnectNotified       uint32            `json:"rrc_connect_notified"`
	RRCRLFCnt                uint32            `json:"rrc_rlf_cnt"`
	RRCRLFInfoPending        uint32            `json:"rrc_rlf_info_pending"`
	RRCConsecutiveKOsDL      uint32            `json:"rrc_consecutive_kos_dl"`
	RRCConsecutiveKOsUL      uint32            `json:"rrc_consecutive_kos_ul"`
	RRCHasTMSI               uint32            `json:"rrc_has_tmsi"`
	RRCMTMSI                 uint32            `json:"rrc_m_tmsi"`
	RRCMMEC                  uint32            `json:"rrc_mmec"`
	RRCEstablishmentCause    uint32            `json:"rrc_establishment_cause"`
	RRCTransactionID         uint32            `json:"rrc_transaction_id"`
	RRCActivityTimerRunning  uint32            `json:"rrc_activity_timer_running"`
	RRCActivityTimerElapsed  uint32            `json:"rrc_activity_timer_elapsed"`
	RRCActivityTimerDuration uint32            `json:"rrc_activity_timer_duration"`
	RRCPhyDLRLFTimerRunning  uint32            `json:"rrc_phy_dl_rlf_timer_running"`
	RRCPhyDLRLFTimerElapsed  uint32            `json:"rrc_phy_dl_rlf_timer_elapsed"`
	RRCPhyDLRLFTimerDuration uint32            `json:"rrc_phy_dl_rlf_timer_duration"`
	RRCPhyULRLFTimerRunning  uint32            `json:"rrc_phy_ul_rlf_timer_running"`
	RRCPhyULRLFTimerElapsed  uint32            `json:"rrc_phy_ul_rlf_timer_elapsed"`
	RRCPhyULRLFTimerDuration uint32            `json:"rrc_phy_ul_rlf_timer_duration"`
	RRCRLCRLFTimerRunning    uint32            `json:"rrc_rlc_rlf_timer_running"`
	RRCRLCRLFTimerElapsed    uint32            `json:"rrc_rlc_rlf_timer_elapsed"`
	RRCRLCRLFTimerDuration   uint32            `json:"rrc_rlc_rlf_timer_duration"`
	RRCLastULMsgBytes        uint32            `json:"rrc_last_ul_msg_bytes"`
	RRCEUTRACapUnpacked      uint32            `json:"rrc_eutra_capabilities_unpacked"`
	RRCReleaseCause          string            `json:"rrc_release_cause"`
	RRCConReqRx              uint32            `json:"rrc_con_req_rx"`
	RRCConSetupTx            uint32            `json:"rrc_con_setup_tx"`
	RRCConSetupCompleteRx    uint32            `json:"rrc_con_setup_complete_rx"`
	RRCConRejectTx           uint32            `json:"rrc_con_reject_tx"`
	RRCConReestReqRx         uint32            `json:"rrc_con_reest_req_rx"`
	RRCConReestTx            uint32            `json:"rrc_con_reest_tx"`
	RRCConReestCompleteRx    uint32            `json:"rrc_con_reest_complete_rx"`
	RRCConReestRejectTx      uint32            `json:"rrc_con_reest_reject_tx"`
	RRCConReconfTx           uint32            `json:"rrc_con_reconf_tx"`
	RRCConReconfCompleteRx   uint32            `json:"rrc_con_reconf_complete_rx"`
	RRCConReleaseTx          uint32            `json:"rrc_con_release_tx"`
	RRCSecModeCmdTx          uint32            `json:"rrc_security_mode_command_tx"`
	RRCSecModeCompleteRx     uint32            `json:"rrc_security_mode_complete_rx"`
	RRCSecModeFailureRx      uint32            `json:"rrc_security_mode_failure_rx"`
	RRCUeCapEnquiryTx        uint32            `json:"rrc_ue_cap_enquiry_tx"`
	RRCUeCapInfoRx           uint32            `json:"rrc_ue_cap_info_rx"`
	RRCUeInfoReqTx           uint32            `json:"rrc_ue_info_req_tx"`
	RRCUeInfoRespRx          uint32            `json:"rrc_ue_info_resp_rx"`
	RRCMaxRLCRetx            uint32            `json:"rrc_max_rlc_retx"`
	RRCProtocolFail          uint32            `json:"rrc_protocol_fail"`
	BearerList               []BearerContainer `json:"bearer_list"`
}

func (u *UEContainer) UnmarshalJSON(data []byte) error {
	type alias UEContainer
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if inner, ok := raw["ue_container"]; ok {
		return json.Unmarshal(inner, (*alias)(u))
	}
	return json.Unmarshal(data, (*alias)(u))
}

type BearerContainer struct {
	BearerID        uint32  `json:"bearer_id"`
	QCI             uint32  `json:"qci"`
	DLTotalBytes    uint64  `json:"dl_total_bytes"`
	ULTotalBytes    uint64  `json:"ul_total_bytes"`
	DLLatency       float64 `json:"dl_latency"`
	ULLatency       float64 `json:"ul_latency"`
	DLBufferedBytes uint32  `json:"dl_buffered_bytes"`
	ULBufferedBytes uint32  `json:"ul_buffered_bytes"`
}

func (b *BearerContainer) UnmarshalJSON(data []byte) error {
	type alias BearerContainer
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if inner, ok := raw["bearer_container"]; ok {
		return json.Unmarshal(inner, (*alias)(b))
	}
	return json.Unmarshal(data, (*alias)(b))
}

func parseUint64(raw json.RawMessage) (uint64, error) {
	var num json.Number
	if err := json.Unmarshal(raw, &num); err != nil {
		return 0, err
	}
	if i, err := num.Int64(); err == nil {
		if i < 0 {
			return 0, fmt.Errorf("negative value")
		}
		return uint64(i), nil
	}
	f, err := num.Float64()
	if err != nil {
		return 0, err
	}
	if f < 0 {
		return 0, fmt.Errorf("negative value")
	}
	return uint64(f), nil
}
