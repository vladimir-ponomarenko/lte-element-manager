package domain

import (
	"encoding/json"
	"testing"
)

func TestS1APContainer_UnmarshalOK(t *testing.T) {
	var c S1APContainer
	in := []byte(`{"s1ap_status":"ready","s1ap_status_code":1,"nas_ul_msgs":6}`)
	if err := json.Unmarshal(in, &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Status != "ready" || c.StatusCode != 1 {
		t.Fatalf("unexpected status: %+v", c)
	}
	if c.Counters["nas_ul_msgs"] != 6 {
		t.Fatalf("missing counter")
	}
}

func TestS1APContainer_UnmarshalBadCounter(t *testing.T) {
	var c S1APContainer
	if err := json.Unmarshal([]byte(`{"s1ap_status":"x","s1ap_status_code":1,"nas_ul_msgs":"nope"}`), &c); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRRCContainer_UnmarshalBadValue(t *testing.T) {
	var c RRCContainer
	if err := json.Unmarshal([]byte(`{"rrc_total_ues":"nope"}`), &c); err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseUint64_Branches(t *testing.T) {
	if _, err := parseUint64(json.RawMessage(`-1`)); err == nil {
		t.Fatalf("expected error")
	}
	if got, err := parseUint64(json.RawMessage(`1`)); err != nil || got != 1 {
		t.Fatalf("unexpected: %d %v", got, err)
	}
	if got, err := parseUint64(json.RawMessage(`1.2`)); err != nil || got != 1 {
		t.Fatalf("unexpected: %d %v", got, err)
	}
	if _, err := parseUint64(json.RawMessage(`1e309`)); err == nil {
		t.Fatalf("expected error")
	}
}

func TestContainerUnmarshal_OuterAndInnerForms(t *testing.T) {
	var cell CellContainer
	if err := json.Unmarshal([]byte(`{"carrier_id":1,"pci":2,"nof_rach":3}`), &cell); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cell.CarrierID != 1 || cell.PCI != 2 || cell.NoFRACH != 3 {
		t.Fatalf("unexpected: %+v", cell)
	}

	var ue UEContainer
	if err := json.Unmarshal([]byte(`{"ue_container":{"ue_rnti":70}}`), &ue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ue.RNTI != 70 {
		t.Fatalf("unexpected ue: %+v", ue)
	}

	var bearer BearerContainer
	if err := json.Unmarshal([]byte(`{"bearer_container":{"bearer_id":3,"qci":9}}`), &bearer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bearer.BearerID != 3 || bearer.QCI != 9 {
		t.Fatalf("unexpected bearer: %+v", bearer)
	}

	var cell2 CellContainer
	if err := json.Unmarshal([]byte(`{"cell_container":{"carrier_id":1,"pci":2,"nof_rach":3}}`), &cell2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cell2.CarrierID != 1 || cell2.PCI != 2 || cell2.NoFRACH != 3 {
		t.Fatalf("unexpected: %+v", cell2)
	}

	var ue2 UEContainer
	if err := json.Unmarshal([]byte(`{"ue_rnti":71}`), &ue2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ue2.RNTI != 71 {
		t.Fatalf("unexpected ue: %+v", ue2)
	}

	var bearer2 BearerContainer
	if err := json.Unmarshal([]byte(`{"bearer_id":4,"qci":7}`), &bearer2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bearer2.BearerID != 4 || bearer2.QCI != 7 {
		t.Fatalf("unexpected bearer: %+v", bearer2)
	}
}

func TestRRCContainer_UnmarshalOK(t *testing.T) {
	var c RRCContainer
	if err := json.Unmarshal([]byte(`{"rrc_total_ues":1,"rrc_connected_ues":2}`), &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counters["rrc_total_ues"] != 1 || c.Counters["rrc_connected_ues"] != 2 {
		t.Fatalf("unexpected counters: %v", c.Counters)
	}
}
