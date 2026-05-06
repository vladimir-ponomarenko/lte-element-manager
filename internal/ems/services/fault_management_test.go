package services

import (
	"testing"

	"lte-element-manager/internal/ems/fcaps/alarms"
)

func TestAlarmLogReaderMapsS1Line(t *testing.T) {
	alarm, action, ok := mapSrsENBAlarmLine("S1 Setup failed: unknown PLMN", "SubNetwork=srsRAN/ManagedElement=enb1")
	if !ok {
		t.Fatalf("expected alarm mapping")
	}
	if action != "raise" {
		t.Fatalf("unexpected action: %s", action)
	}
	if alarm.Code != alarms.AlarmS1Down {
		t.Fatalf("unexpected alarm code: %s", alarm.Code)
	}
}

func TestAlarmLogReaderMapsClearLine(t *testing.T) {
	_, action, ok := mapSrsENBAlarmLine("S1 setup cleared", "SubNetwork=srsRAN/ManagedElement=enb1")
	if !ok {
		t.Fatalf("expected clear mapping")
	}
	if action != "clear" {
		t.Fatalf("unexpected action: %s", action)
	}
}
