package netconf

import (
	"strings"
	"testing"

	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/metrics"
)

func TestBuildMetricsReply_EmptyStore(t *testing.T) {
	store := metrics.NewStore()
	got, err := buildMetricsReply(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "<data/>" {
		t.Fatalf("unexpected reply: %s", got)
	}
}

func TestBuildMetricsReply_OK(t *testing.T) {
	store := metrics.NewStore()
	store.Update(domain.MetricSample{
		RawJSON: `{
  "type":"enb_metrics",
  "enb_serial":"x",
  "timestamp":"1.000000",
  "flag": true,
  "cell_list":[{"carrier_id":0,"pci":1}],
  "s1ap_container":{"nas_ul_msgs":"6"}
}`,
	})

	got, err := buildMetricsReply(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `<data xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">`) {
		t.Fatalf("missing netconf data xmlns: %s", got)
	}
	if !strings.Contains(got, `<enb_metrics xmlns="urn:ems:enb:metrics">`) {
		t.Fatalf("missing module xmlns: %s", got)
	}
	if !strings.Contains(got, "<enb_serial>x</enb_serial>") {
		t.Fatalf("missing enb_serial: %s", got)
	}
	if !strings.Contains(got, "<nas_ul_msgs>6</nas_ul_msgs>") {
		t.Fatalf("missing nas_ul_msgs: %s", got)
	}
	if !strings.Contains(got, "<flag>true</flag>") {
		t.Fatalf("missing bool: %s", got)
	}
}

func TestBuildMetricsReply_InvalidJSON(t *testing.T) {
	store := metrics.NewStore()
	store.Update(domain.MetricSample{RawJSON: "{"})
	_, err := buildMetricsReply(store)
	if err == nil {
		t.Fatalf("expected error")
	}
}
