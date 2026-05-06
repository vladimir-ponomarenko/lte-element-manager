package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lte-element-manager/internal/ems/configuration/srsranconf"
)

func TestStore_EditAndCommit(t *testing.T) {
	dir := t.TempDir()
	enbPath := filepath.Join(dir, "enb.conf")
	rrPath := filepath.Join(dir, "rr.conf")
	sibPath := filepath.Join(dir, "sib.conf")
	rbPath := filepath.Join(dir, "rb.conf")

	enb := `[enb]
enb_id = 0x19A
mcc = 001
mnc = 01
mme_addr = 10.10.1.2
gtp_bind_addr = 10.10.1.11
s1c_bind_addr = 10.10.1.11
s1c_bind_port = 0
n_prb = 50
[rf]
tx_gain = 80
[expert]
enb_serial = ENB-A`
	rr := `cell_list =
(
  {
    cell_id = 0x01;
    tac = 0x0007;
    pci = 1;
    dl_earfcn = 3350;
  }
);
`
	sib := `sib1 = { q_rx_lev_min = -65; cell_barred = "NotBarred"; };
sib2 = {
  rr_config_common_sib = {
    rach_cnfg = { num_ra_preambles = 52; preamble_init_rx_target_pwr = -104; pwr_ramping_step = 6; };
    pcch_cnfg = { default_paging_cycle = 32; };
    ul_pwr_ctrl = { p0_nominal_pusch = -85; alpha = 0.7; p0_nominal_pucch = -107; };
  };
  ue_timers_and_constants = { t300 = 2000; t301 = 100; t310 = 200; n310 = 1; t311 = 10000; };
};`
	rb := `qci_config = (
{ qci = 7; pdcp_config = { discard_timer = -1; pdcp_sn_size = 12; }; rlc_config = { ul_am = { t_poll_retx = 120; max_retx_thresh = 16; }; dl_am = { t_reordering = 50; }; }; logical_channel_config = { priority = 13; }; }
);`
	if err := os.WriteFile(enbPath, []byte(enb), 0o644); err != nil {
		t.Fatalf("write enb: %v", err)
	}
	if err := os.WriteFile(rrPath, []byte(rr), 0o644); err != nil {
		t.Fatalf("write rr: %v", err)
	}
	if err := os.WriteFile(sibPath, []byte(sib), 0o644); err != nil {
		t.Fatalf("write sib: %v", err)
	}
	if err := os.WriteFile(rbPath, []byte(rb), 0o644); err != nil {
		t.Fatalf("write rb: %v", err)
	}

	s, err := NewStore(enbPath, rrPath, sibPath, rbPath)
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

func TestStore_CommitTouchesOnlyEditedLeaves(t *testing.T) {
	dir := t.TempDir()
	enbPath := filepath.Join(dir, "enb.conf")
	rrPath := filepath.Join(dir, "rr.conf")
	sibPath := filepath.Join(dir, "sib.conf")
	rbPath := filepath.Join(dir, "rb.conf")

	enb := `[enb]
enb_id = 0x19A
mcc = 001
mnc = 01
mme_addr = 10.10.1.2
gtp_bind_addr = 10.10.1.11
s1c_bind_addr = 10.10.1.11
s1c_bind_port = 0
n_prb = 50
[rf]
device_args = fail_on_disconnect=false,trx_timeout_ms=3000,id=enb1,tx_port=tcp://*:2000,rx_port=tcp://srsue:2001,base_srate=23.04e6
tx_gain = 80
[expert]
enb_serial = ENB-A`
	rr := `cell_list =
(
  {
    cell_id = 0x01;
    tac = 0x0007;
    pci = 1;
    dl_earfcn = 3350;
  }
);
`
	sib := `sib1 = { q_rx_lev_min = -65; cell_barred = "NotBarred"; };
sib2 = {
  rr_config_common_sib = {
    rach_cnfg = { num_ra_preambles = 52; preamble_init_rx_target_pwr = -104; pwr_ramping_step = 6; };
    pcch_cnfg = { default_paging_cycle = 32; };
    ul_pwr_ctrl = { p0_nominal_pusch = -85; alpha = 0.7; p0_nominal_pucch = -107; };
  };
  ue_timers_and_constants = { t300 = 2000; t301 = 100; t310 = 200; n310 = 1; t311 = 10000; };
};
sib3 = { cell_reselection_common = { q_hyst = 2; }; };`
	rb := `qci_config = (
{ qci = 7; pdcp_config = { discard_timer = -1; pdcp_sn_size = 12; }; rlc_config = { ul_am = { t_poll_retx = 120; max_retx_thresh = 16; }; dl_am = { t_reordering = 50; }; }; logical_channel_config = { priority = 13; bucket_size_duration = 100; }; enb_specific = { dl_max_retx_thresh = 32; }; },
{ qci = 9; pdcp_config = { discard_timer = 150; pdcp_sn_size = 12; }; rlc_config = { ul_am = { t_poll_retx = 120; max_retx_thresh = 16; }; dl_am = { t_reordering = 50; }; }; logical_channel_config = { priority = 11; }; }
);
srb1_5g_config = { rlc_config = { ul_am = { max_retx_thres = 8; }; }; };`

	if err := os.WriteFile(enbPath, []byte(enb), 0o644); err != nil {
		t.Fatalf("write enb: %v", err)
	}
	if err := os.WriteFile(rrPath, []byte(rr), 0o644); err != nil {
		t.Fatalf("write rr: %v", err)
	}
	if err := os.WriteFile(sibPath, []byte(sib), 0o644); err != nil {
		t.Fatalf("write sib: %v", err)
	}
	if err := os.WriteFile(rbPath, []byte(rb), 0o644); err != nil {
		t.Fatalf("write rb: %v", err)
	}

	s, err := NewStore(enbPath, rrPath, sibPath, rbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	_, err = s.Edit(map[string]any{"mcc": "250"})
	if err != nil {
		t.Fatalf("Edit(mcc): %v", err)
	}
	if _, err := s.Commit(); err != nil {
		t.Fatalf("Commit(mcc): %v", err)
	}
	enbAfter, _ := os.ReadFile(enbPath)
	if !strings.Contains(string(enbAfter), "tx_port=tcp://*:2000,rx_port=tcp://srsue:2001") {
		t.Fatalf("device_args unexpectedly changed: %s", string(enbAfter))
	}

	_, err = s.Edit(map[string]any{"tx_gain": 70.5, "n_prb": 75.0, "mme_addr": "10.10.9.9"})
	if err != nil {
		t.Fatalf("Edit(enb leaves): %v", err)
	}
	if _, err := s.Commit(); err != nil {
		t.Fatalf("Commit(enb leaves): %v", err)
	}
	enbAfter, _ = os.ReadFile(enbPath)
	for _, expected := range []string{
		"tx_gain = 70.5",
		"n_prb = 75",
		"mme_addr = 10.10.9.9",
		"tx_port=tcp://*:2000,rx_port=tcp://srsue:2001",
	} {
		if !strings.Contains(string(enbAfter), expected) {
			t.Fatalf("enb expected content %q missing: %s", expected, string(enbAfter))
		}
	}

	beforeRB, _ := os.ReadFile(rbPath)
	_, err = s.Edit(map[string]any{"cell_barred": "Barred"})
	if err != nil {
		t.Fatalf("Edit(cell_barred): %v", err)
	}
	if _, err := s.Commit(); err != nil {
		t.Fatalf("Commit(cell_barred): %v", err)
	}
	sibAfter, _ := os.ReadFile(sibPath)
	if !strings.Contains(string(sibAfter), `cell_barred = "Barred";`) {
		t.Fatalf("cell_barred was not updated: %s", string(sibAfter))
	}
	for _, preserved := range []string{"sib3", "q_hyst = 2", "ue_timers_and_constants"} {
		if !strings.Contains(string(sibAfter), preserved) {
			t.Fatalf("sib unmanaged content %q was lost: %s", preserved, string(sibAfter))
		}
	}
	afterRB, _ := os.ReadFile(rbPath)
	if string(beforeRB) != string(afterRB) {
		t.Fatalf("rb.conf changed unexpectedly")
	}

	_, err = s.Edit(map[string]any{"qci_profiles[9].priority": int32(10)})
	if err != nil {
		t.Fatalf("Edit(qci priority): %v", err)
	}
	if _, err := s.Commit(); err != nil {
		t.Fatalf("Commit(qci priority): %v", err)
	}
	rbAfterQCI, _ := os.ReadFile(rbPath)
	if !strings.Contains(string(rbAfterQCI), "priority = 10;") {
		t.Fatalf("qci priority was not updated: %s", string(rbAfterQCI))
	}
	for _, preserved := range []string{"qci = 7", "priority = 13", "bucket_size_duration = 100", "enb_specific", "srb1_5g_config", "max_retx_thres = 8"} {
		if !strings.Contains(string(rbAfterQCI), preserved) {
			t.Fatalf("rb unmanaged content %q was lost: %s", preserved, string(rbAfterQCI))
		}
	}
	if _, err := srsranconf.ParseENB(enbPath); err != nil {
		t.Fatalf("generated enb.conf is not parseable: %v", err)
	}
	if _, err := srsranconf.ParseRR(rrPath); err != nil {
		t.Fatalf("generated rr.conf is not parseable: %v", err)
	}
	if _, err := srsranconf.ParseSIB(sibPath); err != nil {
		t.Fatalf("generated sib.conf is not parseable: %v", err)
	}
	if _, err := srsranconf.ParseRB(rbPath); err != nil {
		t.Fatalf("generated rb.conf is not parseable: %v", err)
	}
}
