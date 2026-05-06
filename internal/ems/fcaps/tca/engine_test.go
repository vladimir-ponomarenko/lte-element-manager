package tca

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/domain/nrm"
	domainpm "lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/fcaps/alarms"
	pmfcaps "lte-element-manager/internal/ems/fcaps/pm"
)

func TestLowThroughputRequiresConnectedUEs(t *testing.T) {
	mgr := alarms.NewManager(alarms.NewStore())
	engine := NewEngine(mgr, shortConfig(), zerolog.Nop())
	now := time.Unix(100, 0)

	engine.Evaluate(report(now, enbDN(), map[string]float64{
		domainpm.CanonicalRRCConnectedUES: 0,
		domainpm.CanonicalUEDLBitrate:     0,
		domainpm.CanonicalUEULBitrate:     0,
	}))
	engine.Evaluate(report(now.Add(2*time.Second), enbDN(), map[string]float64{
		domainpm.CanonicalRRCConnectedUES: 0,
		domainpm.CanonicalUEDLBitrate:     0,
		domainpm.CanonicalUEULBitrate:     0,
	}))

	if got := mgr.Active(); len(got) != 0 {
		t.Fatalf("expected no low-throughput alarm without connected UE, got %+v", got)
	}
}

func TestLowThroughputRaiseAndClear(t *testing.T) {
	mgr := alarms.NewManager(alarms.NewStore())
	engine := NewEngine(mgr, shortConfig(), zerolog.Nop())
	now := time.Unix(100, 0)

	engine.Evaluate(report(now, enbDN(), map[string]float64{
		domainpm.CanonicalRRCConnectedUES: 1,
		domainpm.CanonicalUEDLBitrate:     0,
		domainpm.CanonicalUEULBitrate:     0,
	}))
	engine.Evaluate(report(now.Add(2*time.Second), enbDN(), map[string]float64{
		domainpm.CanonicalRRCConnectedUES: 1,
		domainpm.CanonicalUEDLBitrate:     0,
		domainpm.CanonicalUEULBitrate:     0,
	}))

	active := mgr.Active()
	if len(active) != 1 || active[0].AlarmID != alarms.AlarmLowThroughputActiveUsers {
		t.Fatalf("expected low throughput alarm, got %+v", active)
	}

	engine.Evaluate(report(now.Add(3*time.Second), enbDN(), map[string]float64{
		domainpm.CanonicalRRCConnectedUES: 1,
		domainpm.CanonicalUEDLBitrate:     2_000_000,
	}))
	engine.Evaluate(report(now.Add(5*time.Second), enbDN(), map[string]float64{
		domainpm.CanonicalRRCConnectedUES: 1,
		domainpm.CanonicalUEDLBitrate:     2_000_000,
	}))

	if got := mgr.Active(); len(got) != 0 {
		t.Fatalf("expected cleared low throughput alarm, got %+v", got)
	}
}

func TestBadSignalHysteresis(t *testing.T) {
	mgr := alarms.NewManager(alarms.NewStore())
	engine := NewEngine(mgr, shortConfig(), zerolog.Nop())
	now := time.Unix(200, 0)
	dn := cellDN()

	engine.Evaluate(report(now, dn, map[string]float64{domainpm.CanonicalUEULSNR: -1}))
	engine.Evaluate(report(now.Add(2*time.Second), dn, map[string]float64{domainpm.CanonicalUEULSNR: -2}))
	if active := mgr.Active(); len(active) != 1 || active[0].AlarmID != alarms.AlarmBadSignalCondition {
		t.Fatalf("expected bad signal alarm, got %+v", active)
	}

	engine.Evaluate(report(now.Add(3*time.Second), dn, map[string]float64{domainpm.CanonicalUEULSNR: 2}))
	engine.Evaluate(report(now.Add(5*time.Second), dn, map[string]float64{domainpm.CanonicalUEULSNR: 2}))
	if active := mgr.Active(); len(active) != 1 {
		t.Fatalf("expected hysteresis to keep bad signal active, got %+v", active)
	}

	engine.Evaluate(report(now.Add(6*time.Second), dn, map[string]float64{domainpm.CanonicalUEULSNR: 6}))
	engine.Evaluate(report(now.Add(8*time.Second), dn, map[string]float64{domainpm.CanonicalUEULSNR: 6}))
	if active := mgr.Active(); len(active) != 0 {
		t.Fatalf("expected bad signal clear after clear threshold TTT, got %+v", active)
	}
}

func TestAllRequiredMetricAlarmsRaise(t *testing.T) {
	cases := []struct {
		name   string
		dn     nrm.DN
		values map[string]float64
		alarm  string
	}{
		{name: "s1 down", dn: enbDN(), values: map[string]float64{domainpm.CanonicalS1APReady: 0}, alarm: alarms.AlarmS1InterfaceDown},
		{name: "nas signaling", dn: enbDN(), values: map[string]float64{domainpm.CanonicalNASDLDrop: 1}, alarm: alarms.AlarmNASSignalingLoss},
		{name: "nas security", dn: enbDN(), values: map[string]float64{domainpm.CanonicalNASULSecUnknown: 1}, alarm: alarms.AlarmNASSecurityMismatch},
		{name: "nas parse", dn: enbDN(), values: map[string]float64{domainpm.CanonicalNASULParseFail: 1}, alarm: alarms.AlarmNASParsingFailure},
		{name: "rrc protocol", dn: enbDN(), values: map[string]float64{domainpm.CanonicalRRCProtocolFail: 1}, alarm: alarms.AlarmRRCProtocolError},
		{name: "rrc reject", dn: enbDN(), values: map[string]float64{domainpm.CanonicalRRCConRejectTX: 1}, alarm: alarms.AlarmRRCConnectionRejection},
		{name: "core service reject", dn: enbDN(), values: map[string]float64{domainpm.CanonicalNASDLServiceRej: 1}, alarm: alarms.AlarmCoreServiceReject},
		{name: "paging", dn: enbDN(), values: map[string]float64{domainpm.CanonicalRRCPagingFail: 1}, alarm: alarms.AlarmPagingCapacityExceeded},
		{name: "rlc retx", dn: enbDN(), values: map[string]float64{domainpm.CanonicalRRCMaxRLCRetx: 1}, alarm: alarms.AlarmRLCMaxRetransmissions},
		{name: "bler", dn: cellDN(), values: map[string]float64{domainpm.CanonicalUEDLBLER: 0.20}, alarm: alarms.AlarmHighBLER},
		{name: "rlf storm", dn: cellDN(), values: map[string]float64{domainpm.CanonicalUERRCRLFCnt: 6}, alarm: alarms.AlarmRadioLinkFailureStorm},
		{name: "rf interference", dn: cellDN(), values: map[string]float64{domainpm.CanonicalUEULPUCCHNI: -80}, alarm: alarms.AlarmRFInterferenceDetected},
		{name: "inactivity", dn: cellDN(), values: map[string]float64{domainpm.CanonicalUERRCInactivity: 1}, alarm: alarms.AlarmUEInactivityCleanup},
		{name: "bearer congestion", dn: cellDN(), values: map[string]float64{"bearer.7.dl_buffered_bytes": 600000}, alarm: alarms.AlarmBearerCongestion},
		{name: "phr", dn: cellDN(), values: map[string]float64{domainpm.CanonicalUEULPHR: 0}, alarm: alarms.AlarmPowerHeadroomCritical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := alarms.NewManager(alarms.NewStore())
			engine := NewEngine(mgr, immediateConfig(), zerolog.Nop())
			now := time.Unix(100, 0)
			engine.Evaluate(report(now, tc.dn, tc.values))
			engine.Evaluate(report(now.Add(2*time.Second), tc.dn, tc.values))
			active := mgr.Active()
			if len(active) != 1 || active[0].AlarmID != tc.alarm {
				t.Fatalf("expected %s, got %+v", tc.alarm, active)
			}
		})
	}
}

func TestRepeatedViolationIncrementsCountWithoutDuplicateActiveRecord(t *testing.T) {
	mgr := alarms.NewManager(alarms.NewStore())
	engine := NewEngine(mgr, immediateConfig(), zerolog.Nop())
	now := time.Unix(500, 0)
	dn := cellDN()

	engine.Evaluate(report(now, dn, map[string]float64{domainpm.CanonicalUEDLBLER: 0.20}))
	engine.Evaluate(report(now.Add(2*time.Second), dn, map[string]float64{domainpm.CanonicalUEDLBLER: 0.30}))
	engine.Evaluate(report(now.Add(3*time.Second), dn, map[string]float64{domainpm.CanonicalUEDLBLER: 0.30}))

	active := mgr.Active()
	if len(active) != 1 {
		t.Fatalf("expected one active alarm, got %+v", active)
	}
	if active[0].Count != 2 {
		t.Fatalf("expected dedup count 2, got %+v", active[0])
	}
}

func shortConfig() Config {
	cfg := DemoSafeConfig()
	for name, rule := range cfg.Rules {
		rule.RaiseDuration = time.Second
		rule.ClearDuration = time.Second
		cfg.Rules[name] = rule
	}
	return cfg
}

func immediateConfig() Config {
	cfg := DemoSafeConfig()
	for name, rule := range cfg.Rules {
		rule.RaiseDuration = time.Nanosecond
		rule.ClearDuration = time.Nanosecond
		cfg.Rules[name] = rule
	}
	return cfg
}

func report(at time.Time, dn nrm.DN, values map[string]float64) pmfcaps.Report {
	out := make(map[string]pmfcaps.Value, len(values))
	for k, v := range values {
		out[k] = pmfcaps.Value{Value: v}
	}
	return pmfcaps.Report{
		End:  at,
		ByDN: map[nrm.DN]map[string]pmfcaps.Value{dn: out},
	}
}

func enbDN() nrm.DN {
	return "SubNetwork=srsRAN/ManagedElement=enb1/ENBFunction=1"
}

func cellDN() nrm.DN {
	return "SubNetwork=srsRAN/ManagedElement=enb1/ENBFunction=1/EUtranCell=1"
}
