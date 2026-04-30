package netconfcm

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"lte-element-manager/internal/ems/configuration"
)

type valueKind int

const (
	kindString valueKind = iota
	kindInt32
	kindUint32
	kindFloat64
	kindBool
)

type leafBinding struct {
	Path    string
	Key     string
	Kind    valueKind
	Get     func(configuration.EditableConfig) any
	Include func(configuration.EditableConfig) bool
}

type registry struct {
	leaves map[string]leafBinding
}

const (
	baseSubNetworkPath = "/_3gpp-common-managed-element:SubNetwork"
	baseManagedElement = baseSubNetworkPath + "/_3gpp-common-managed-element:ManagedElement"
	baseENBFunction    = baseManagedElement + "/_3gpp-common-managed-element:ENBFunction"
	baseEUtranCell     = baseENBFunction + "/_3gpp-common-managed-element:EUtranCell"
	baseScheduler      = baseENBFunction + "/srsran-vendor-ext:scheduler"
	baseSIB            = baseENBFunction + "/srsran-vendor-ext:sib"
	baseTimers         = baseSIB + "/srsran-vendor-ext:ue_timers_and_constants"
	baseQCIProfiles    = baseENBFunction + "/srsran-vendor-ext:qci_profiles"
	baseExpert         = baseENBFunction + "/srsran-vendor-ext:expert"
)

var (
	pathPredicatesRE = regexp.MustCompile(`\[[^\]]+\]`)
	qciPathRE        = regexp.MustCompile(`qci_profiles\[[^\]]*qci='([^']+)'\]/(?:srsran-vendor-ext:)?([A-Za-z0-9_]+)$`)
)

func newRegistry() *registry {
	bindings := []leafBinding{
		{Path: baseENBFunction + "/_3gpp-common-managed-element:enb_id", Key: "enb_id", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.ENBID }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.ENBID })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:mcc", Key: "mcc", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.MCC }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.MCC })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:mnc", Key: "mnc", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.MNC }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.MNC })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:mme_addr", Key: "mme_addr", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.MMEAddr }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.MMEAddr })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:gtp_bind_addr", Key: "gtp_bind_addr", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.GTPBindAddr }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.GTPBindAddr })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:s1c_bind_addr", Key: "s1c_bind_addr", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.S1CBindAddr }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.S1CBindAddr })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:s1c_bind_port", Key: "s1c_bind_port", Kind: kindUint32, Get: func(c configuration.EditableConfig) any { return c.S1CBindPort }, Include: nonZeroUint32(func(c configuration.EditableConfig) uint32 { return c.S1CBindPort })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:n_prb", Key: "n_prb", Kind: kindUint32, Get: func(c configuration.EditableConfig) any { return c.NPRB }, Include: nonZeroUint32(func(c configuration.EditableConfig) uint32 { return c.NPRB })},
		{Path: baseENBFunction + "/_3gpp-common-managed-element:tm", Key: "tm", Kind: kindUint32, Get: func(c configuration.EditableConfig) any { return c.TM }, Include: nonZeroUint32(func(c configuration.EditableConfig) uint32 { return c.TM })},

		{Path: baseEUtranCell + "/_3gpp-common-managed-element:id", Key: "cell_id", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.CellID }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.CellID })},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:tac", Key: "tac", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.TAC }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.TAC })},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:dl_earfcn", Key: "dl_earfcn", Kind: kindUint32, Get: func(c configuration.EditableConfig) any { return c.DLEARFCN }, Include: nonZeroUint32(func(c configuration.EditableConfig) uint32 { return c.DLEARFCN })},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:pci", Key: "pci", Kind: kindUint32, Get: func(c configuration.EditableConfig) any { return c.PCI }, Include: alwaysInclude},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:ho_active", Key: "ho_active", Kind: kindBool, Get: func(c configuration.EditableConfig) any { return c.HOActive }, Include: alwaysInclude},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:a3_offset", Key: "a3_offset", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.A3Offset }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.A3Offset })},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:time_to_trigger", Key: "time_to_trigger", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.TimeToTrigger }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.TimeToTrigger })},
		{Path: baseEUtranCell + "/_3gpp-common-managed-element:hysteresis", Key: "hysteresis", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.Hysteresis }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.Hysteresis })},

		{Path: baseENBFunction + "/srsran-vendor-ext:enb_serial", Key: "enb_serial", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.ENBSerial }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.ENBSerial })},
		{Path: baseENBFunction + "/srsran-vendor-ext:tx_gain", Key: "tx_gain", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.TXGain }, Include: alwaysInclude},
		{Path: baseENBFunction + "/srsran-vendor-ext:rx_gain", Key: "rx_gain", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.RXGain }, Include: alwaysInclude},
		{Path: baseENBFunction + "/srsran-vendor-ext:time_adv_nsamples", Key: "time_adv_nsamples", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.TimeAdvNSamples }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.TimeAdvNSamples })},
		{Path: baseENBFunction + "/srsran-vendor-ext:device_name", Key: "device_name", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.DeviceName }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.DeviceName })},
		{Path: baseENBFunction + "/srsran-vendor-ext:device_args", Key: "device_args", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.DeviceArgs }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.DeviceArgs })},

		{Path: baseScheduler + "/srsran-vendor-ext:sched_policy", Key: "sched_policy", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.SchedPolicy }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.SchedPolicy })},
		{Path: baseScheduler + "/srsran-vendor-ext:pdsch_max_mcs", Key: "pdsch_max_mcs", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.PDSCHMaxMCS }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.PDSCHMaxMCS })},
		{Path: baseScheduler + "/srsran-vendor-ext:pusch_max_mcs", Key: "pusch_max_mcs", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.PUSCHMaxMCS }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.PUSCHMaxMCS })},
		{Path: baseScheduler + "/srsran-vendor-ext:target_bler", Key: "target_bler", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.TargetBLER }, Include: nonZeroFloat64(func(c configuration.EditableConfig) float64 { return c.TargetBLER })},
		{Path: baseScheduler + "/srsran-vendor-ext:min_nof_ctrl_symbols", Key: "min_nof_ctrl_symbols", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.MinCtrlSymbols }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.MinCtrlSymbols })},
		{Path: baseScheduler + "/srsran-vendor-ext:max_nof_ctrl_symbols", Key: "max_nof_ctrl_symbols", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.MaxCtrlSymbols }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.MaxCtrlSymbols })},

		{Path: baseSIB + "/srsran-vendor-ext:q_rx_lev_min", Key: "q_rx_lev_min", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.QRxLevMin }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.QRxLevMin })},
		{Path: baseSIB + "/srsran-vendor-ext:cell_barred", Key: "cell_barred", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.CellBarred }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.CellBarred })},
		{Path: baseSIB + "/srsran-vendor-ext:num_ra_preambles", Key: "num_ra_preambles", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.NumRAPreambles }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.NumRAPreambles })},
		{Path: baseSIB + "/srsran-vendor-ext:preamble_init_rx_target_pwr", Key: "preamble_init_rx_target_pwr", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.PreambleInitRxTargetPwr }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.PreambleInitRxTargetPwr })},
		{Path: baseSIB + "/srsran-vendor-ext:pwr_ramping_step", Key: "pwr_ramping_step", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.PwrRampingStep }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.PwrRampingStep })},
		{Path: baseSIB + "/srsran-vendor-ext:reference_signal_power", Key: "reference_signal_power", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.ReferenceSignalPower }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.ReferenceSignalPower })},
		{Path: baseSIB + "/srsran-vendor-ext:p0_nominal_pusch", Key: "p0_nominal_pusch", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.P0NominalPUSCH }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.P0NominalPUSCH })},
		{Path: baseSIB + "/srsran-vendor-ext:p0_nominal_pucch", Key: "p0_nominal_pucch", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.P0NominalPUCCH }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.P0NominalPUCCH })},
		{Path: baseSIB + "/srsran-vendor-ext:alpha", Key: "alpha", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.Alpha }, Include: nonZeroFloat64(func(c configuration.EditableConfig) float64 { return c.Alpha })},
		{Path: baseSIB + "/srsran-vendor-ext:default_paging_cycle", Key: "default_paging_cycle", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.DefaultPagingCycle }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.DefaultPagingCycle })},
		{Path: baseTimers + "/srsran-vendor-ext:t300", Key: "t300", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.T300 }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.T300 })},
		{Path: baseTimers + "/srsran-vendor-ext:t301", Key: "t301", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.T301 }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.T301 })},
		{Path: baseTimers + "/srsran-vendor-ext:t310", Key: "t310", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.T310 }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.T310 })},
		{Path: baseTimers + "/srsran-vendor-ext:n310", Key: "n310", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.N310 }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.N310 })},
		{Path: baseTimers + "/srsran-vendor-ext:t311", Key: "t311", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.T311 }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.T311 })},

		{Path: baseExpert + "/srsran-vendor-ext:pusch_max_its", Key: "pusch_max_its", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.PUSCHMaxIts }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.PUSCHMaxIts })},
		{Path: baseExpert + "/srsran-vendor-ext:nr_pusch_max_its", Key: "nr_pusch_max_its", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.NRPUSCHMaxIts }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.NRPUSCHMaxIts })},
		{Path: baseExpert + "/srsran-vendor-ext:pusch_8bit_decoder", Key: "pusch_8bit_decoder", Kind: kindBool, Get: func(c configuration.EditableConfig) any { return c.PUSCH8bitDecoder }, Include: alwaysInclude},
		{Path: baseExpert + "/srsran-vendor-ext:nof_phy_threads", Key: "nof_phy_threads", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.NofPHYThreads }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.NofPHYThreads })},
		{Path: baseExpert + "/srsran-vendor-ext:metrics_period_secs", Key: "metrics_period_secs", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.MetricsPeriodSecs }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.MetricsPeriodSecs })},
		{Path: baseExpert + "/srsran-vendor-ext:tx_amplitude", Key: "tx_amplitude", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.TXAmplitude }, Include: nonZeroFloat64(func(c configuration.EditableConfig) float64 { return c.TXAmplitude })},
		{Path: baseExpert + "/srsran-vendor-ext:rrc_inactivity_timer", Key: "rrc_inactivity_timer", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.RRCInactivityTimer }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.RRCInactivityTimer })},
		{Path: baseExpert + "/srsran-vendor-ext:rlf_release_timer_ms", Key: "rlf_release_timer_ms", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.RLFReleaseTimerMs }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.RLFReleaseTimerMs })},
		{Path: baseExpert + "/srsran-vendor-ext:eea_pref_list", Key: "eea_pref_list", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.EEAPrefList }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.EEAPrefList })},
		{Path: baseExpert + "/srsran-vendor-ext:eia_pref_list", Key: "eia_pref_list", Kind: kindString, Get: func(c configuration.EditableConfig) any { return c.EIAPrefList }, Include: nonEmptyString(func(c configuration.EditableConfig) string { return c.EIAPrefList })},
		{Path: baseExpert + "/srsran-vendor-ext:gtpu_tunnel_timeout", Key: "gtpu_tunnel_timeout", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.GTPUTunnelTimeout }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.GTPUTunnelTimeout })},
		{Path: baseExpert + "/srsran-vendor-ext:s1_setup_max_retries", Key: "s1_setup_max_retries", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.S1SetupMaxRetries }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.S1SetupMaxRetries })},
		{Path: baseExpert + "/srsran-vendor-ext:s1_connect_timer", Key: "s1_connect_timer", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.S1ConnectTimer }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.S1ConnectTimer })},
		{Path: baseExpert + "/srsran-vendor-ext:rx_gain_offset", Key: "rx_gain_offset", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.RXGainOffset }, Include: nonZeroFloat64(func(c configuration.EditableConfig) float64 { return c.RXGainOffset })},
		{Path: baseExpert + "/srsran-vendor-ext:use_cedron_f_est_alg", Key: "use_cedron_f_est_alg", Kind: kindBool, Get: func(c configuration.EditableConfig) any { return c.UseCedronFEstAlg }, Include: alwaysInclude},
		{Path: baseExpert + "/srsran-vendor-ext:rlf_min_ul_snr_estim", Key: "rlf_min_ul_snr_estim", Kind: kindFloat64, Get: func(c configuration.EditableConfig) any { return c.RLFMinULSNREstim }, Include: nonZeroFloat64(func(c configuration.EditableConfig) float64 { return c.RLFMinULSNREstim })},
		{Path: baseExpert + "/srsran-vendor-ext:max_mac_dl_kos", Key: "max_mac_dl_kos", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.MaxMacDLKOs }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.MaxMacDLKOs })},
		{Path: baseExpert + "/srsran-vendor-ext:max_mac_ul_kos", Key: "max_mac_ul_kos", Kind: kindInt32, Get: func(c configuration.EditableConfig) any { return c.MaxMacULKOs }, Include: nonZeroInt32(func(c configuration.EditableConfig) int32 { return c.MaxMacULKOs })},
	}

	out := &registry{leaves: make(map[string]leafBinding, len(bindings))}
	for _, b := range bindings {
		out.leaves[b.Path] = b
	}
	return out
}

func normalizePath(path string) string {
	path = pathPredicatesRE.ReplaceAllString(path, "")
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if idx := strings.Index(part, ":"); idx >= 0 {
			parts[i] = part[idx+1:]
		}
	}
	return strings.Join(parts, "/")
}

func (r *registry) resolve(path string, isKey bool, value string) (string, any, bool, error) {
	normalized := normalizePath(path)
	if isStructuralKeyPath(normalized, isKey) {
		return "", nil, true, nil
	}
	if m := qciPathRE.FindStringSubmatch(path); len(m) == 3 {
		key := strings.TrimSpace(m[1])
		field := strings.TrimSpace(m[2])
		if field == "qci" {
			return "", nil, true, nil
		}
		flatKey := fmt.Sprintf("qci_profiles[%s].%s", key, field)
		parsed, err := parseValue(qciFieldKind(field), value)
		if err != nil {
			return "", nil, false, err
		}
		return flatKey, parsed, false, nil
	}
	binding, ok := r.leaves[normalized]
	if !ok {
		for candidatePath, candidate := range r.leaves {
			if normalizePath(candidatePath) == normalized {
				binding = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return "", nil, false, fmt.Errorf("unsupported editable path: %s", path)
	}
	parsed, err := parseValue(binding.Kind, value)
	if err != nil {
		return "", nil, false, fmt.Errorf("%s: %w", binding.Key, err)
	}
	return binding.Key, parsed, false, nil
}

func isStructuralKeyPath(path string, isKey bool) bool {
	return isKey
}

func qciFieldKind(field string) valueKind {
	switch field {
	case "discard_timer", "pdcp_sn_size", "t_poll_retx", "max_retx_thresh", "t_reordering", "priority":
		return kindInt32
	default:
		return kindString
	}
}

func parseValue(kind valueKind, value string) (any, error) {
	switch kind {
	case kindString:
		return value, nil
	case kindInt32:
		x, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("expected int32, got %q", value)
		}
		return int32(x), nil
	case kindUint32:
		x, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("expected uint32, got %q", value)
		}
		return uint32(x), nil
	case kindFloat64:
		x, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("expected number, got %q", value)
		}
		return x, nil
	case kindBool:
		x, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("expected boolean, got %q", value)
		}
		return x, nil
	default:
		return nil, fmt.Errorf("unsupported value kind")
	}
}

func (r *registry) currentValue(cfg configuration.EditableConfig, key string) (any, bool) {
	if strings.HasPrefix(key, "qci_profiles[") {
		return currentQCIFieldValue(cfg, key)
	}
	for _, b := range r.leaves {
		if b.Key == key {
			return b.Get(cfg), true
		}
	}
	return nil, false
}

func currentQCIFieldValue(cfg configuration.EditableConfig, key string) (any, bool) {
	re := regexp.MustCompile(`^qci_profiles\[(\d+)\]\.([A-Za-z0-9_]+)$`)
	m := re.FindStringSubmatch(key)
	if len(m) != 3 {
		return nil, false
	}
	qci, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil {
		return nil, false
	}
	field := m[2]
	for _, p := range cfg.QCIProfiles {
		if p.QCI != int32(qci) {
			continue
		}
		switch field {
		case "discard_timer":
			return p.DiscardTimer, true
		case "pdcp_sn_size":
			return p.PDCPSNSize, true
		case "t_poll_retx":
			return p.TPollRetx, true
		case "max_retx_thresh":
			return p.MaxRetxThresh, true
		case "t_reordering":
			return p.TReordering, true
		case "priority":
			return p.Priority, true
		}
	}
	return nil, false
}

func (r *registry) render(ids IDs, cfg configuration.EditableConfig) map[string]any {
	enb := map[string]any{
		"id": ids.ENBFunctionID,
	}
	setRenderedLeaves(enb, cfg, r, baseENBFunction)

	cell := map[string]any{}
	setRenderedLeaves(cell, cfg, r, baseEUtranCell)
	if len(cell) > 0 {
		enb["EUtranCell"] = []any{cell}
	}

	scheduler := map[string]any{}
	setRenderedLeaves(scheduler, cfg, r, baseScheduler)
	if len(scheduler) > 0 {
		enb["srsran-vendor-ext:scheduler"] = scheduler
	}

	sib := map[string]any{}
	setRenderedLeaves(sib, cfg, r, baseSIB)
	timers := map[string]any{}
	setRenderedLeaves(timers, cfg, r, baseTimers)
	if len(timers) > 0 {
		sib["ue_timers_and_constants"] = timers
	}
	if len(sib) > 0 {
		enb["srsran-vendor-ext:sib"] = sib
	}

	profiles := renderQCIProfiles(cfg.QCIProfiles)
	if len(profiles) > 0 {
		enb["srsran-vendor-ext:qci_profiles"] = profiles
	}

	expert := map[string]any{}
	setRenderedLeaves(expert, cfg, r, baseExpert)
	if len(expert) > 0 {
		enb["srsran-vendor-ext:expert"] = expert
	}

	managed := map[string]any{
		"id":          ids.ManagedElement,
		"ENBFunction": []any{enb},
	}
	subnetwork := map[string]any{
		"id":             ids.SubNetwork,
		"ManagedElement": []any{managed},
	}
	return map[string]any{
		"_3gpp-common-managed-element:SubNetwork": []any{subnetwork},
	}
}

func setRenderedLeaves(dst map[string]any, cfg configuration.EditableConfig, r *registry, prefix string) {
	paths := make([]string, 0)
	for path, binding := range r.leaves {
		if !strings.HasPrefix(path, prefix+"/") {
			continue
		}
		rel := strings.TrimPrefix(path, prefix+"/")
		if strings.Contains(rel, "/") {
			continue
		}
		if !binding.Include(cfg) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		binding := r.leaves[path]
		leafName := renderedChildName(prefix, path)
		dst[leafName] = renderValue(binding, cfg)
	}
}

func renderValue(binding leafBinding, cfg configuration.EditableConfig) any {
	value := binding.Get(cfg)
	if binding.Kind != kindFloat64 {
		return value
	}
	f, ok := value.(float64)
	if !ok {
		return value
	}
	// RFC 7951 encodes decimal64 as a JSON string to preserve exact decimal
	// representation and avoid binary floating-point ambiguity.
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func renderQCIProfiles(profiles []configuration.QCIProfile) []any {
	if len(profiles) == 0 {
		return nil
	}
	sorted := append([]configuration.QCIProfile(nil), profiles...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].QCI < sorted[j].QCI })
	out := make([]any, 0, len(sorted))
	for _, p := range sorted {
		out = append(out, map[string]any{
			"qci":             p.QCI,
			"discard_timer":   p.DiscardTimer,
			"pdcp_sn_size":    p.PDCPSNSize,
			"t_poll_retx":     p.TPollRetx,
			"max_retx_thresh": p.MaxRetxThresh,
			"t_reordering":    p.TReordering,
			"priority":        p.Priority,
		})
	}
	return out
}

func lastPathSegment(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

func renderedChildName(parentPath, childPath string) string {
	child := lastPathSegment(childPath)
	if modulePrefix(parentPath) != modulePrefix(childPath) {
		return child
	}
	return stripModulePrefix(child)
}

func modulePrefix(path string) string {
	name := lastPathSegment(path)
	if idx := strings.Index(name, ":"); idx >= 0 {
		return name[:idx]
	}
	return ""
}

func stripModulePrefix(name string) string {
	if idx := strings.Index(name, ":"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func alwaysInclude(configuration.EditableConfig) bool { return true }

func nonEmptyString(fn func(configuration.EditableConfig) string) func(configuration.EditableConfig) bool {
	return func(cfg configuration.EditableConfig) bool {
		return strings.TrimSpace(fn(cfg)) != ""
	}
}

func nonZeroInt32(fn func(configuration.EditableConfig) int32) func(configuration.EditableConfig) bool {
	return func(cfg configuration.EditableConfig) bool {
		return fn(cfg) != 0
	}
}

func nonZeroUint32(fn func(configuration.EditableConfig) uint32) func(configuration.EditableConfig) bool {
	return func(cfg configuration.EditableConfig) bool {
		return fn(cfg) != 0
	}
}

func nonZeroFloat64(fn func(configuration.EditableConfig) float64) func(configuration.EditableConfig) bool {
	return func(cfg configuration.EditableConfig) bool {
		return fn(cfg) != 0
	}
}
