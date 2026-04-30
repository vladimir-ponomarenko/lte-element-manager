package services

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain/nrm"
	domainpm "lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/fcaps/alarms"
	pmfcaps "lte-element-manager/internal/ems/fcaps/pm"
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

func TestTCAServiceRaisesLowThroughputAlarm(t *testing.T) {
	mgr := alarms.NewManager(alarms.NewStore())
	svc := NewTCAService(bus.New(10), mgr, zerolog.Nop())
	svc.Duration = time.Second

	dn := nrm.DN("SubNetwork=srsRAN/ManagedElement=enb1/ENBFunction=1/EUtranCell=1")
	svc.lowSince[string(dn)] = time.Now().Add(-2 * time.Second)
	svc.evaluateReport(pmfcaps.Report{
		End: time.Now(),
		ByDN: map[nrm.DN]map[string]pmfcaps.Value{
			dn: {
				domainpm.CanonicalUEDLBitrate: {Value: 0},
				domainpm.CanonicalUEULBitrate: {Value: 0},
			},
		},
	})

	active := mgr.Active()
	if len(active) != 1 {
		t.Fatalf("expected one active TCA alarm, got %d", len(active))
	}
	if active[0].AlarmID != alarms.AlarmLowThroughput {
		t.Fatalf("unexpected alarm: %+v", active[0])
	}
}
