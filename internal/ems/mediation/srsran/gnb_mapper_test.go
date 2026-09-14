package srsran

import (
	"testing"

	"lte-element-manager/internal/ems/domain/canonical"
)

func TestMapper_MapGNB(t *testing.T) {
	raw := `{"type":"gnb_metrics","timestamp":1,"gnb_id":"gnb-1","ngap_container":{"ngap_status":"connected","ngap_ue_release":2},"rrc_container":{"rrc_connected_ues":1},"cell_list":[{"nci":"123","pci":10,"ue_list":[{"ue_rnti":70,"dl_bitrate":100,"ul_bitrate":50,"ul_sinr":8}]}]}`
	samples, err := (&Mapper{}).Map(raw)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("samples = %d, want node/cell/ue", len(samples))
	}
	if samples[1].Scope != "cell:nr=123" {
		t.Fatalf("cell scope = %q", samples[1].Scope)
	}
	if samples[0].Metrics["ngap.ready"].Value != 1 {
		t.Fatal("NGAP connected state was not normalized")
	}
	if metric := samples[0].Metrics["rrc.rrc_connected_ues"]; metric.Type != canonical.Gauge || metric.Value != 1 {
		t.Fatalf("RRC connected UE metric is not a gauge: %#v", metric)
	}
}
