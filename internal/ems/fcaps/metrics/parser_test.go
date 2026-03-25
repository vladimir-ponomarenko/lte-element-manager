package metrics

import "testing"

func TestParseEnbMetrics_OK(t *testing.T) {
	payload := []byte(`{
  "type": "enb_metrics",
  "enb_serial": "enb-1",
  "timestamp": 1773771188.471,
  "s1ap_container": {"s1ap_status": "ready", "s1ap_status_code": 1, "nas_ul_msgs": 6},
  "rrc_container": {"rrc_total_ues": 1, "rrc_connected_ues": 1, "rrc_state_idle": 0},
  "cell_list": [
    {"carrier_id": 0, "pci": 1, "nof_rach": 0, "ue_list": [
      {"ue_rnti": 70, "dl_bitrate": 0.0, "ul_bitrate": 0.0, "rrc_state_str": "connected",
       "bearer_list": [{"bearer_id": 5, "qci": 9, "dl_total_bytes": 100, "ul_total_bytes": 200, "dl_latency": 1.1, "ul_latency": 2.2, "dl_buffered_bytes": 3, "ul_buffered_bytes": 4}]
      }
    ]}
  ]
}`)
	m, err := ParseEnbMetrics(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Type != "enb_metrics" {
		t.Fatalf("wrong type")
	}
	if m.S1AP.Status != "ready" {
		t.Fatalf("wrong s1ap status")
	}
	if m.S1AP.Counters["nas_ul_msgs"] != 6 {
		t.Fatalf("missing s1ap counter")
	}
	if len(m.CellList) != 1 || len(m.CellList[0].UEList) != 1 {
		t.Fatalf("unexpected cell or ue list length")
	}
	if len(m.CellList[0].UEList[0].BearerList) != 1 {
		t.Fatalf("unexpected bearer list length")
	}
}

func TestParseEnbMetrics_InvalidType(t *testing.T) {
	payload := []byte(`{"type":"wrong","timestamp":1}`)
	_, err := ParseEnbMetrics(payload)
	if err == nil {
		t.Fatalf("expected error")
	}
}
