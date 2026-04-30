//go:build netconf && cgo

package netconfcm

import (
	"encoding/json"
	"testing"

	"lte-element-manager/internal/ems/configuration"
)

func TestRenderedArtifactsValidateWithLibyang(t *testing.T) {
	r := newRegistry()
	cfg := configuration.EditableConfig{
		ENBSerial:         "ENB-0x19A-001-01-SibSutis&Yadro",
		ENBID:             "0x19A",
		MCC:               "001",
		MNC:               "01",
		MMEAddr:           "10.10.1.10",
		GTPBindAddr:       "10.10.1.11",
		S1CBindAddr:       "10.10.1.11",
		S1CBindPort:       0,
		NPRB:              50,
		TM:                1,
		DeviceName:        "zmq",
		DeviceArgs:        "fail_on_disconnect=false,trx_timeout_ms=3000,id=enb1,tx_port=tcp://*:2000,rx_port=tcp://srsue:2001,base_srate=23.04e6",
		TXGain:            80,
		RXGain:            30,
		TimeAdvNSamples:   "auto",
		CellID:            "0x01",
		TAC:               "0x0007",
		DLEARFCN:          3350,
		PCI:               1,
		CellBarred:        "NotBarred",
		SchedPolicy:       "time_rr",
		QCIProfiles:       []configuration.QCIProfile{{QCI: 9, DiscardTimer: 150, PDCPSNSize: 12, TPollRetx: 120, MaxRetxThresh: 16, TReordering: 50, Priority: 11}},
		NofPHYThreads:     3,
		MetricsPeriodSecs: 1,
	}
	raw, err := json.Marshal(r.render(IDs{SubNetwork: "srsRAN", ManagedElement: "enb1", ENBFunctionID: "1"}, cfg))
	if err != nil {
		t.Fatalf("marshal rendered artifact: %v", err)
	}
	if err := validateYANGJSON("../../../yang", string(raw)); err != nil {
		t.Fatalf("rendered artifact failed libyang validation: %v\njson=%s", err, string(raw))
	}
}
