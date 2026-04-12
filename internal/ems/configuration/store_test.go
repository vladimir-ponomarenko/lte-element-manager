package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_EditAndCommit(t *testing.T) {
	dir := t.TempDir()
	enbPath := filepath.Join(dir, "enb.conf")
	rrPath := filepath.Join(dir, "rr.conf")

	enb := `[enb]
mcc = 001
mnc = 01
n_prb = 50
[rf]
tx_gain = 80
[expert]
enb_serial = ENB-A`
	rr := `cell_list =
(
  {
    pci = 1;
    dl_earfcn = 3350;
  }
);
`
	if err := os.WriteFile(enbPath, []byte(enb), 0o644); err != nil {
		t.Fatalf("write enb: %v", err)
	}
	if err := os.WriteFile(rrPath, []byte(rr), 0o644); err != nil {
		t.Fatalf("write rr: %v", err)
	}

	s, err := NewStore(enbPath, rrPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	_, err = s.Edit(map[string]any{
		"mcc":       "250",
		"n_prb":     75.0,
		"tx_gain":   77.5,
		"pci":       2.0,
		"dl_earfcn": 3400.0,
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	cfg, err := s.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if cfg.MCC != "250" || cfg.NPRB != 75 || cfg.PCI != 2 || cfg.DLEARFCN != 3400 {
		t.Fatalf("unexpected running: %+v", cfg)
	}
}
