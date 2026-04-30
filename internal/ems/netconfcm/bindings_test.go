package netconfcm

import (
	"reflect"
	"testing"

	"lte-element-manager/internal/ems/configuration"
)

func TestRegistryResolveScalarLeaf(t *testing.T) {
	r := newRegistry()
	key, value, structural, err := r.resolve(baseENBFunction+"/_3gpp-common-managed-element:mcc", false, "250")
	if err != nil {
		t.Fatalf("resolve mcc: %v", err)
	}
	if structural {
		t.Fatalf("mcc should not be structural")
	}
	if key != "mcc" {
		t.Fatalf("unexpected key %q", key)
	}
	if got, ok := value.(string); !ok || got != "250" {
		t.Fatalf("unexpected value %#v", value)
	}
}

func TestRegistryResolveStructuralKey(t *testing.T) {
	r := newRegistry()
	key, value, structural, err := r.resolve(baseENBFunction+"/_3gpp-common-managed-element:id", true, "1")
	if err != nil {
		t.Fatalf("resolve structural id: %v", err)
	}
	if key != "" || value != nil || !structural {
		t.Fatalf("expected structural key to be skipped, got key=%q value=%#v structural=%v", key, value, structural)
	}
}

func TestRegistryResolveQCILeaf(t *testing.T) {
	r := newRegistry()
	key, value, structural, err := r.resolve(baseQCIProfiles+"[qci='7']/srsran-vendor-ext:discard_timer", false, "150")
	if err != nil {
		t.Fatalf("resolve qci leaf: %v", err)
	}
	if structural {
		t.Fatalf("qci discard_timer should not be structural")
	}
	if key != "qci_profiles[7].discard_timer" {
		t.Fatalf("unexpected key %q", key)
	}
	if got, ok := value.(int32); !ok || got != 150 {
		t.Fatalf("unexpected qci value %#v", value)
	}
}

func TestRegistryRenderProducesStableNRMTree(t *testing.T) {
	r := newRegistry()
	cfg := configuration.EditableConfig{
		ENBSerial:        "ENB-1",
		MCC:              "001",
		MNC:              "01",
		NPRB:             50,
		DeviceName:       "zmq",
		DeviceArgs:       "fail_on_disconnect=false,trx_timeout_ms=3000,id=enb1,tx_port=tcp://*:2000,rx_port=tcp://srsue:2001,base_srate=23.04e6",
		TXGain:           80,
		RXGain:           30,
		PCI:              11,
		CellBarred:       "NotBarred",
		SchedPolicy:      "time_rr",
		QCIProfiles:      []configuration.QCIProfile{{QCI: 9, DiscardTimer: 150, PDCPSNSize: 12, TPollRetx: 120, MaxRetxThresh: 16, TReordering: 50, Priority: 11}},
		UseCedronFEstAlg: true,
	}

	data := r.render(IDs{SubNetwork: "srsRAN", ManagedElement: "enb1", ENBFunctionID: "1"}, cfg)

	root, ok := data["_3gpp-common-managed-element:SubNetwork"].([]any)
	if !ok || len(root) != 1 {
		t.Fatalf("unexpected root tree: %#v", data)
	}
	subnet := root[0].(map[string]any)
	if subnet["id"] != "srsRAN" {
		t.Fatalf("unexpected subnetwork id %#v", subnet["id"])
	}
	meList := subnet["ManagedElement"].([]any)
	enbList := meList[0].(map[string]any)["ENBFunction"].([]any)
	enb := enbList[0].(map[string]any)

	if enb["mcc"] != "001" || enb["mnc"] != "01" {
		t.Fatalf("missing rendered PLMN: %#v", enb)
	}
	if _, ok := enb["device_args"]; ok {
		t.Fatalf("vendor augment leaf rendered without module prefix: %#v", enb)
	}
	if enb["srsran-vendor-ext:enb_serial"] != "ENB-1" {
		t.Fatalf("missing rendered serial: %#v", enb["srsran-vendor-ext:enb_serial"])
	}
	if enb["srsran-vendor-ext:device_args"] != cfg.DeviceArgs {
		t.Fatalf("missing rendered device_args: %#v", enb["srsran-vendor-ext:device_args"])
	}
	if enb["srsran-vendor-ext:device_name"] != "zmq" {
		t.Fatalf("missing rendered device_name: %#v", enb["srsran-vendor-ext:device_name"])
	}
	if enb["srsran-vendor-ext:tx_gain"] != "80" || enb["srsran-vendor-ext:rx_gain"] != "30" {
		t.Fatalf("missing rendered RF gain leaves: %#v", enb)
	}
	if !reflect.DeepEqual(enb["srsran-vendor-ext:scheduler"], map[string]any{"sched_policy": "time_rr"}) {
		t.Fatalf("unexpected scheduler render: %#v", enb["srsran-vendor-ext:scheduler"])
	}
	cell := enb["EUtranCell"].([]any)[0].(map[string]any)
	if cell["pci"] != uint32(11) {
		t.Fatalf("unexpected EUtranCell render: %#v", cell)
	}
	sib := enb["srsran-vendor-ext:sib"].(map[string]any)
	if sib["cell_barred"] != "NotBarred" {
		t.Fatalf("unexpected sib render: %#v", sib)
	}
	profiles := enb["srsran-vendor-ext:qci_profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("unexpected qci render: %#v", profiles)
	}
	profile := profiles[0].(map[string]any)
	if profile["qci"] != int32(9) || profile["discard_timer"] != int32(150) {
		t.Fatalf("unexpected qci profile: %#v", profile)
	}
	expert := enb["srsran-vendor-ext:expert"].(map[string]any)
	if expert["use_cedron_f_est_alg"] != true {
		t.Fatalf("unexpected expert render: %#v", expert)
	}
}

func TestValidateEditOptions(t *testing.T) {
	if err := validateEditOptions("merge", "test-then-set", "rollback-on-error"); err != nil {
		t.Fatalf("expected valid edit options: %v", err)
	}
	if err := validateEditOptions("merge", "", "continue-on-error"); err == nil {
		t.Fatalf("expected continue-on-error to be rejected")
	}
}

func TestRegistryCoversEditableConfigKeys(t *testing.T) {
	r := newRegistry()
	got := map[string]bool{}
	for _, binding := range r.leaves {
		got[binding.Key] = true
	}
	for _, key := range []string{
		"enb_serial", "enb_id", "mcc", "mnc", "mme_addr", "gtp_bind_addr", "s1c_bind_addr", "s1c_bind_port", "n_prb", "tm",
		"device_name", "device_args", "tx_gain", "rx_gain", "time_adv_nsamples",
		"sched_policy", "pdsch_max_mcs", "pusch_max_mcs", "target_bler", "min_nof_ctrl_symbols", "max_nof_ctrl_symbols",
		"cell_id", "tac", "dl_earfcn", "pci", "ho_active", "a3_offset", "time_to_trigger", "hysteresis",
		"q_rx_lev_min", "cell_barred", "num_ra_preambles", "preamble_init_rx_target_pwr", "pwr_ramping_step",
		"reference_signal_power", "p0_nominal_pusch", "p0_nominal_pucch", "alpha", "default_paging_cycle",
		"t300", "t301", "t310", "n310", "t311",
		"pusch_max_its", "nr_pusch_max_its", "pusch_8bit_decoder", "nof_phy_threads", "metrics_period_secs", "tx_amplitude",
		"rrc_inactivity_timer", "rlf_release_timer_ms", "eea_pref_list", "eia_pref_list", "gtpu_tunnel_timeout",
		"s1_setup_max_retries", "s1_connect_timer", "rx_gain_offset", "use_cedron_f_est_alg", "rlf_min_ul_snr_estim",
		"max_mac_dl_kos", "max_mac_ul_kos",
	} {
		if !got[key] {
			t.Fatalf("registry is missing editable key %q", key)
		}
	}

	for _, field := range []string{"discard_timer", "pdcp_sn_size", "t_poll_retx", "max_retx_thresh", "t_reordering", "priority"} {
		key, _, structural, err := r.resolve(baseQCIProfiles+"[qci='7']/srsran-vendor-ext:"+field, false, "1")
		if err != nil {
			t.Fatalf("registry qci field %s is not resolvable: %v", field, err)
		}
		if structural || key != "qci_profiles[7]."+field {
			t.Fatalf("unexpected qci resolution for %s: key=%q structural=%v", field, key, structural)
		}
	}
}
