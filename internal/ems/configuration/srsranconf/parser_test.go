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
