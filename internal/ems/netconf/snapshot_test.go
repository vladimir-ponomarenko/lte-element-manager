package netconf

import (
	"encoding/json"
	"testing"
	"time"

	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/domain/nrm"
	"lte-element-manager/internal/ems/fcaps/alarms"
)

func TestBuildCombinedSnapshotIncludesActiveAlarms(t *testing.T) {
	reg, err := nrm.New(nrm.Config{SubNetwork: "srsRAN", ManagedElement: "enb1", ENBFunctionID: "1"})
	if err != nil {
		t.Fatalf("nrm registry: %v", err)
	}
	store := alarms.NewStore()
	store.Upsert(time.Unix(1, 0).UTC(), "uds", alarms.Normalize("uds", "SubNetwork=srsRAN/ManagedElement=enb1/ENBFunction=1", domain.Alarm{Message: "socket gone"}))

	raw, err := BuildCombinedSnapshot(SnapshotConfig{SubNetwork: "srsRAN", ManagedElement: "enb1", ENBFunctionID: "1"}, reg, nil, "{}", store)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("snapshot json: %v", err)
	}
	sub := root["_3gpp-common-managed-element:SubNetwork"].([]any)[0].(map[string]any)
	me := sub["ManagedElement"].([]any)[0].(map[string]any)
	enb := me["ENBFunction"].([]any)[0].(map[string]any)
	fm := enb["ems-fault-management:fault_management"].(map[string]any)
	active := fm["active_alarm"].([]any)
	if len(active) != 1 {
		t.Fatalf("expected one active alarm, got %#v", active)
	}
	alarm := active[0].(map[string]any)
	if alarm["alarm_id"] != alarms.AlarmUDSDisconnected {
		t.Fatalf("unexpected alarm: %#v", alarm)
	}
	if alarm["perceived_severity"] != alarms.SeverityCritical {
		t.Fatalf("unexpected severity: %#v", alarm)
	}
	if alarm["occurrence_count"] != "1" {
		t.Fatalf("unexpected occurrence_count: %#v", alarm)
	}
}
