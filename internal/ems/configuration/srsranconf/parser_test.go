package srsranconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseENB(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "enb.conf")
	content := `
[enb]
mcc = 001
mnc = 01
n_prb = 50

[rf]
tx_gain = 80
device_args = fail_on_disconnect=false,trx_timeout_ms=3000,id=enb1,tx_port=tcp://*:2000,rx_port=tcp://srsue:2001,base_srate=23.04e6

[expert]
enb_serial = ENB-0x19A-001-01-SibSutis&Yadro
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ParseENB(p)
	if err != nil {
		t.Fatalf("ParseENB failed: %v", err)
	}
	if cfg.Serial == "" || cfg.MCC != "001" || cfg.MNC != "01" || cfg.NPRB != 50 || cfg.TXGain != 80 {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.DeviceArgs != "fail_on_disconnect=false,trx_timeout_ms=3000,id=enb1,tx_port=tcp://*:2000,rx_port=tcp://srsue:2001,base_srate=23.04e6" {
		t.Fatalf("device_args was parsed as comment-truncated value: %q", cfg.DeviceArgs)
	}
}

func TestParseRR(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rr.conf")
	content := `
cell_list =
(
  {
    pci = 2;
    dl_earfcn = 3350;
    meas_cell_list =
    (
      {
        dl_earfcn = 6300;
        pci = 99;
      }
    );
  }
);
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ParseRR(p)
	if err != nil {
		t.Fatalf("ParseRR failed: %v", err)
	}
	if cfg.PCI != 2 || cfg.DLEARFCN != 3350 {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}
