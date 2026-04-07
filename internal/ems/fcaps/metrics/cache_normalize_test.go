package metrics

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestNormalizeSnapshotJSON_StructureAndTypes(t *testing.T) {
	raw := `{
  "type":"enb_metrics",
  "enb_serial":"x",
  "timestamp": 1,
  "s1ap_container": {"nas_ul_msgs": 6},
  "cell_list": [
    {
      "cell_container": {
        "carrier_id": 0,
        "pci": 1,
        "ue_list": [
          {
            "ue_container": {
              "ue_rnti": 70,
              "dl_cqi": 15,
              "ul_pusch_tpc": 0,
              "bearer_list": [
                {"bearer_container": {"bearer_id": 3, "dl_total_bytes": 5, "ul_total_bytes": 6}}
              ]
            }
          }
        ]
      }
    }
  ]
}`

	normalized, err := normalizeSnapshotJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(normalized), &got); err != nil {
		t.Fatalf("normalized json invalid: %v", err)
	}

	if got["timestamp"] == float64(1) {
		t.Fatalf("expected timestamp to be encoded as a string")
	}
	if s1, ok := got["s1ap_container"].(map[string]any); ok {
		if s1["nas_ul_msgs"] == float64(6) {
			t.Fatalf("expected nas counters to be encoded as strings")
		}
	} else {
		t.Fatalf("missing s1ap_container")
	}

	cellList := got["cell_list"].([]any)
	cell0 := cellList[0].(map[string]any)
	if _, ok := cell0["carrier_id"]; !ok {
		t.Fatalf("expected carrier_id to be lifted to list entry")
	}
	if _, ok := cell0["pci"]; !ok {
		t.Fatalf("expected pci to be lifted to list entry")
	}

	cellContainer := cell0["cell_container"].(map[string]any)
	ue0 := cellContainer["ue_list"].([]any)[0].(map[string]any)
	if _, ok := ue0["ue_rnti"]; !ok {
		t.Fatalf("expected ue_rnti to be lifted to list entry")
	}

	ueContainer := ue0["ue_container"].(map[string]any)
	if ueContainer["dl_cqi"] == float64(15) {
		t.Fatalf("expected dl_cqi to be encoded as a string")
	}

	bearer0 := ueContainer["bearer_list"].([]any)[0].(map[string]any)
	if _, ok := bearer0["bearer_id"]; !ok {
		t.Fatalf("expected bearer_id to be lifted to list entry")
	}
}

func TestWriteSnapshot_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := writeSnapshot(path, "{"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestTypeConverters(t *testing.T) {
	if s, ok := toUintString(float64(2)); !ok || s != "2" {
		t.Fatalf("unexpected uint conv: %q %v", s, ok)
	}
	if s, ok := toInt64String(int64(-1)); !ok || s != "-1" {
		t.Fatalf("unexpected int conv: %q %v", s, ok)
	}
	if _, ok := toUintString("x"); ok {
		t.Fatalf("expected false")
	}
	if _, ok := toDecimalString("x"); ok {
		t.Fatalf("expected false")
	}
}
