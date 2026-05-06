package configuration

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Config rendering is an overlay, not a full-file rewrite.
//
// srsRAN files mix INI and libconfig syntax and contain many advanced knobs
// outside the current CM contract. The EMS owns typed CM state, but commits
// must preserve every non-managed byte in operator files. The renderer below
// therefore formats only the managed leaf value and patches exactly one
// assignment in the existing text using small syntax-aware scanners.

func renderENBConfig(path string, cfg EditableConfig, dirty map[string]struct{}) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := data
	for _, key := range sortedDirtyKeys(dirty) {
		section, confKey, value, ok := enbPatch(key, cfg)
		if !ok {
			continue
		}
		out, err = patchINIAssignment(out, section, confKey, value)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func renderRRConfig(path string, cfg EditableConfig, dirty map[string]struct{}) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := data
	for _, key := range sortedDirtyKeys(dirty) {
		confKey, value, ok := rrPatch(key, cfg)
		if !ok {
			continue
		}
		out, err = patchLibconfigScalar(out, confKey, value)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func renderSIBConfig(path string, cfg EditableConfig, dirty map[string]struct{}) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := data
	for _, key := range sortedDirtyKeys(dirty) {
		confKey, value, ok := sibPatch(key, cfg)
		if !ok {
			continue
		}
		out, err = patchLibconfigScalar(out, confKey, value)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func renderRBConfig(path string, cfg EditableConfig, dirty map[string]struct{}) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := data
	for _, key := range sortedDirtyKeys(dirty) {
		if !strings.HasPrefix(key, "qci_profiles[") {
			continue
		}
		out, err = patchQCIProfile(out, key, cfg)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func sortedDirtyKeys(in map[string]struct{}) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, strings.TrimSpace(k))
	}
	sort.Strings(out)
	return out
}

func enbPatch(key string, cfg EditableConfig) (section, confKey, value string, ok bool) {
	switch key {
	case "enb_id":
		return "enb", "enb_id", cfg.ENBID, true
	case "mcc":
		return "enb", "mcc", cfg.MCC, true
	case "mnc":
		return "enb", "mnc", cfg.MNC, true
	case "mme_addr":
		return "enb", "mme_addr", cfg.MMEAddr, true
	case "gtp_bind_addr":
		return "enb", "gtp_bind_addr", cfg.GTPBindAddr, true
	case "s1c_bind_addr":
		return "enb", "s1c_bind_addr", cfg.S1CBindAddr, true
	case "s1c_bind_port":
		return "enb", "s1c_bind_port", strconv.FormatUint(uint64(cfg.S1CBindPort), 10), true
	case "n_prb":
		return "enb", "n_prb", strconv.FormatUint(uint64(cfg.NPRB), 10), true
	case "tm":
		return "enb", "tm", strconv.FormatUint(uint64(cfg.TM), 10), true
	case "tx_gain":
		return "rf", "tx_gain", trimFloat(cfg.TXGain), true
	case "rx_gain":
		return "rf", "rx_gain", trimFloat(cfg.RXGain), true
	case "time_adv_nsamples":
		return "rf", "time_adv_nsamples", cfg.TimeAdvNSamples, true
	case "device_name":
		return "rf", "device_name", cfg.DeviceName, true
	case "device_args":
		return "rf", "device_args", cfg.DeviceArgs, true
	case "sched_policy":
		return "scheduler", "policy", cfg.SchedPolicy, true
	case "pdsch_max_mcs":
		return "scheduler", "pdsch_max_mcs", strconv.FormatInt(int64(cfg.PDSCHMaxMCS), 10), true
	case "pusch_max_mcs":
		return "scheduler", "pusch_max_mcs", strconv.FormatInt(int64(cfg.PUSCHMaxMCS), 10), true
	case "target_bler":
		return "scheduler", "target_bler", trimFloat(cfg.TargetBLER), true
	case "min_nof_ctrl_symbols":
		return "scheduler", "min_nof_ctrl_symbols", strconv.FormatInt(int64(cfg.MinCtrlSymbols), 10), true
	case "max_nof_ctrl_symbols":
		return "scheduler", "max_nof_ctrl_symbols", strconv.FormatInt(int64(cfg.MaxCtrlSymbols), 10), true
	case "enb_serial":
		return "expert", "enb_serial", cfg.ENBSerial, true
	case "pusch_max_its":
		return "expert", "pusch_max_its", strconv.FormatInt(int64(cfg.PUSCHMaxIts), 10), true
	case "nr_pusch_max_its":
		return "expert", "nr_pusch_max_its", strconv.FormatInt(int64(cfg.NRPUSCHMaxIts), 10), true
	case "pusch_8bit_decoder":
		return "expert", "pusch_8bit_decoder", formatBool(cfg.PUSCH8bitDecoder), true
	case "nof_phy_threads":
		return "expert", "nof_phy_threads", strconv.FormatInt(int64(cfg.NofPHYThreads), 10), true
	case "metrics_period_secs":
		return "expert", "metrics_period_secs", strconv.FormatInt(int64(cfg.MetricsPeriodSecs), 10), true
	case "tx_amplitude":
		return "expert", "tx_amplitude", trimFloat(cfg.TXAmplitude), true
	case "rrc_inactivity_timer":
		return "expert", "rrc_inactivity_timer", strconv.FormatInt(int64(cfg.RRCInactivityTimer), 10), true
	case "rlf_release_timer_ms":
		return "expert", "rlf_release_timer_ms", strconv.FormatInt(int64(cfg.RLFReleaseTimerMs), 10), true
	case "eea_pref_list":
		return "expert", "eea_pref_list", cfg.EEAPrefList, true
	case "eia_pref_list":
		return "expert", "eia_pref_list", cfg.EIAPrefList, true
	case "gtpu_tunnel_timeout":
		return "expert", "gtpu_tunnel_timeout", strconv.FormatInt(int64(cfg.GTPUTunnelTimeout), 10), true
	case "s1_setup_max_retries":
		return "expert", "s1_setup_max_retries", strconv.FormatInt(int64(cfg.S1SetupMaxRetries), 10), true
	case "s1_connect_timer":
		return "expert", "s1_connect_timer", strconv.FormatInt(int64(cfg.S1ConnectTimer), 10), true
	case "rx_gain_offset":
		return "expert", "rx_gain_offset", trimFloat(cfg.RXGainOffset), true
	case "use_cedron_f_est_alg":
		return "expert", "use_cedron_f_est_alg", formatBool(cfg.UseCedronFEstAlg), true
	case "rlf_min_ul_snr_estim":
		return "expert", "rlf_min_ul_snr_estim", trimFloat(cfg.RLFMinULSNREstim), true
	case "max_mac_dl_kos":
		return "expert", "max_mac_dl_kos", strconv.FormatInt(int64(cfg.MaxMacDLKOs), 10), true
	case "max_mac_ul_kos":
		return "expert", "max_mac_ul_kos", strconv.FormatInt(int64(cfg.MaxMacULKOs), 10), true
	default:
		return "", "", "", false
	}
}

func rrPatch(key string, cfg EditableConfig) (confKey, value string, ok bool) {
	switch key {
	case "cell_id":
		return "cell_id", cfg.CellID, true
	case "tac":
		return "tac", cfg.TAC, true
	case "dl_earfcn":
		return "dl_earfcn", strconv.FormatUint(uint64(cfg.DLEARFCN), 10), true
	case "pci":
		return "pci", strconv.FormatUint(uint64(cfg.PCI), 10), true
	case "ho_active":
		return "ho_active", formatBool(cfg.HOActive), true
	case "a3_offset":
		return "a3_offset", strconv.FormatInt(int64(cfg.A3Offset), 10), true
	case "time_to_trigger":
		return "time_to_trigger", strconv.FormatInt(int64(cfg.TimeToTrigger), 10), true
	case "hysteresis":
		return "hysteresis", strconv.FormatInt(int64(cfg.Hysteresis), 10), true
	default:
		return "", "", false
	}
}

func sibPatch(key string, cfg EditableConfig) (confKey, value string, ok bool) {
	switch key {
	case "q_rx_lev_min":
		return "q_rx_lev_min", strconv.FormatInt(int64(cfg.QRxLevMin), 10), true
	case "cell_barred":
		return "cell_barred", strconv.Quote(cfg.CellBarred), true
	case "num_ra_preambles":
		return "num_ra_preambles", strconv.FormatInt(int64(cfg.NumRAPreambles), 10), true
	case "preamble_init_rx_target_pwr":
		return "preamble_init_rx_target_pwr", strconv.FormatInt(int64(cfg.PreambleInitRxTargetPwr), 10), true
	case "pwr_ramping_step":
		return "pwr_ramping_step", strconv.FormatInt(int64(cfg.PwrRampingStep), 10), true
	case "reference_signal_power":
		return "reference_signal_power", strconv.FormatInt(int64(cfg.ReferenceSignalPower), 10), true
	case "p0_nominal_pusch":
		return "p0_nominal_pusch", strconv.FormatInt(int64(cfg.P0NominalPUSCH), 10), true
	case "p0_nominal_pucch":
		return "p0_nominal_pucch", strconv.FormatInt(int64(cfg.P0NominalPUCCH), 10), true
	case "alpha":
		return "alpha", trimFloat(cfg.Alpha), true
	case "default_paging_cycle":
		return "default_paging_cycle", strconv.FormatInt(int64(cfg.DefaultPagingCycle), 10), true
	case "t300":
		return "t300", strconv.FormatInt(int64(cfg.T300), 10), true
	case "t301":
		return "t301", strconv.FormatInt(int64(cfg.T301), 10), true
	case "t310":
		return "t310", strconv.FormatInt(int64(cfg.T310), 10), true
	case "n310":
		return "n310", strconv.FormatInt(int64(cfg.N310), 10), true
	case "t311":
		return "t311", strconv.FormatInt(int64(cfg.T311), 10), true
	default:
		return "", "", false
	}
}

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func patchINIAssignment(data []byte, section, key, value string) ([]byte, error) {
	lines := splitLines(data)
	target := strings.ToLower(strings.TrimSpace(section))
	current := ""
	foundSection := false
	insertAt := len(lines)

	for i, line := range lines {
		if sec, ok := parseINISection(line); ok {
			if foundSection && current == target {
				insertAt = i
				break
			}
			current = sec
			if current == target {
				foundSection = true
				insertAt = i + 1
			}
			continue
		}
		if current != target || isCommentLine(line) {
			continue
		}
		if !lineHasAssignmentKey(line, key) {
			if foundSection {
				insertAt = i + 1
			}
			continue
		}
		lines[i] = replaceAssignmentLine(line, key, value, false)
		return []byte(strings.Join(lines, "")), nil
	}

	newLine := key + " = " + value + "\n"
	if !foundSection {
		if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
			lines[len(lines)-1] += "\n"
		}
		lines = append(lines, "\n["+section+"]\n", newLine)
		return []byte(strings.Join(lines, "")), nil
	}
	lines = insertLine(lines, insertAt, newLine)
	return []byte(strings.Join(lines, "")), nil
}

func patchLibconfigScalar(data []byte, key, value string) ([]byte, error) {
	out, ok := patchLibconfigScalarOptional(data, key, value)
	if !ok {
		return nil, fmt.Errorf("key %q not found in libconfig file", key)
	}
	return out, nil
}

func patchLibconfigScalarOptional(data []byte, key, value string) ([]byte, bool) {
	asn, ok := findAssignment(data, key, 0)
	if !ok {
		return data, false
	}
	valueEnd, ok := findScalarValueEnd(data, asn.ValueStart)
	if !ok {
		return data, false
	}
	var out bytes.Buffer
	out.Grow(len(data) + len(value))
	out.Write(data[:asn.ValueStart])
	out.WriteString(value)
	out.Write(data[valueEnd:])
	return out.Bytes(), true
}

func patchQCIProfile(data []byte, key string, cfg EditableConfig) ([]byte, error) {
	qci, field, ok := parseQCIProfileKey(key)
	if !ok {
		return nil, fmt.Errorf("invalid qci profile key %q", key)
	}
	profile, ok := findQCIProfile(cfg, qci)
	if !ok {
		return nil, fmt.Errorf("qci profile %d is not present in candidate", qci)
	}
	value, containers, err := qciPatch(field, profile)
	if err != nil {
		return nil, err
	}
	start, end, ok := findQCIBlock(data, qci)
	if !ok {
		return nil, fmt.Errorf("qci profile %d not found in rb.conf", qci)
	}
	block := append([]byte(nil), data[start:end]...)
	if patched, changed := patchLibconfigScalarOptional(block, field, value); changed {
		return replaceRange(data, start, end, patched), nil
	}
	patched, err := insertQCIAssignment(block, containers, field+" = "+value+";")
	if err != nil {
		return nil, err
	}
	return replaceRange(data, start, end, patched), nil
}

func findQCIProfile(cfg EditableConfig, qci int32) (QCIProfile, bool) {
	for _, p := range cfg.QCIProfiles {
		if p.QCI == qci {
			return p, true
		}
	}
	return QCIProfile{}, false
}

func qciPatch(field string, p QCIProfile) (value string, containers []string, err error) {
	switch field {
	case "discard_timer":
		return strconv.FormatInt(int64(p.DiscardTimer), 10), []string{"pdcp_config"}, nil
	case "pdcp_sn_size":
		return strconv.FormatInt(int64(p.PDCPSNSize), 10), []string{"pdcp_config"}, nil
	case "t_poll_retx":
		return strconv.FormatInt(int64(p.TPollRetx), 10), []string{"ul_am", "ul_um"}, nil
	case "max_retx_thresh":
		return strconv.FormatInt(int64(p.MaxRetxThresh), 10), []string{"ul_am"}, nil
	case "t_reordering":
		return strconv.FormatInt(int64(p.TReordering), 10), []string{"dl_am", "dl_um"}, nil
	case "priority":
		return strconv.FormatInt(int64(p.Priority), 10), []string{"logical_channel_config"}, nil
	default:
		return "", nil, fmt.Errorf("unsupported qci profile field %q", field)
	}
}

func parseQCIProfileKey(key string) (int32, string, bool) {
	const prefix = "qci_profiles["
	if !strings.HasPrefix(key, prefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	close := strings.IndexByte(rest, ']')
	if close <= 0 || close+1 >= len(rest) || rest[close+1] != '.' {
		return 0, "", false
	}
	qci64, err := strconv.ParseInt(rest[:close], 10, 32)
	if err != nil {
		return 0, "", false
	}
	field := rest[close+2:]
	if field == "" {
		return 0, "", false
	}
	for i := 0; i < len(field); i++ {
		if !isIdent(field[i]) {
			return 0, "", false
		}
	}
	return int32(qci64), field, true
}

func findQCIBlock(data []byte, qci int32) (int, int, bool) {
	anchor, ok := findAssignment(data, "qci_config", 0)
	if !ok {
		return 0, 0, false
	}
	for i := anchor.ValueStart; i < len(data); i++ {
		if isLineCommentStart(data, i) {
			i = skipLine(data, i)
			continue
		}
		if data[i] == '"' {
			next, ok := skipString(data, i)
			if !ok {
				return 0, 0, false
			}
			i = next
			continue
		}
		if data[i] != '{' {
			continue
		}
		end, ok := findMatchingBrace(data, i)
		if !ok {
			return 0, 0, false
		}
		block := data[i : end+1]
		if qciBlockHasKey(block, qci) {
			return i, end + 1, true
		}
		i = end
	}
	return 0, 0, false
}

func qciBlockHasKey(block []byte, qci int32) bool {
	asn, ok := findAssignment(block, "qci", 0)
	if !ok {
		return false
	}
	valueEnd, ok := findScalarValueEnd(block, asn.ValueStart)
	if !ok {
		return false
	}
	got := strings.TrimSpace(string(block[asn.ValueStart:valueEnd]))
	return got == strconv.FormatInt(int64(qci), 10)
}

func insertQCIAssignment(block []byte, containers []string, assignment string) ([]byte, error) {
	for _, name := range containers {
		_, end, ok := findLibconfigContainer(block, name)
		if !ok {
			continue
		}
		indent := closingIndent(block, end)
		line := []byte(indent + "  " + assignment + "\n")
		return insertBytes(block, end, line), nil
	}
	return nil, fmt.Errorf("none of containers %v found in qci profile", containers)
}

func findLibconfigContainer(data []byte, name string) (int, int, bool) {
	searchFrom := 0
	for {
		asn, ok := findAssignment(data, name, searchFrom)
		if !ok {
			return 0, 0, false
		}
		open := skipASCIIWhitespace(data, asn.ValueStart)
		if open >= len(data) || data[open] != '{' {
			searchFrom = asn.ValueStart + 1
			continue
		}
		close, ok := findMatchingBrace(data, open)
		if !ok {
			return 0, 0, false
		}
		return open, close, true
	}
}

func findMatchingBrace(data []byte, open int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(data); i++ {
		if !inString && isLineCommentStart(data, i) {
			i = skipLine(data, i)
			continue
		}
		c := data[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

type assignmentSpan struct {
	KeyStart   int
	KeyEnd     int
	Equal      int
	ValueStart int
}

func findAssignment(data []byte, key string, from int) (assignmentSpan, bool) {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(data); i++ {
		if isLineCommentStart(data, i) {
			i = skipLine(data, i)
			continue
		}
		if data[i] == '"' {
			next, ok := skipString(data, i)
			if !ok {
				return assignmentSpan{}, false
			}
			i = next
			continue
		}
		if !isIdentStart(data[i]) {
			continue
		}
		start := i
		for i < len(data) && isIdent(data[i]) {
			i++
		}
		token := string(data[start:i])
		j := skipASCIIWhitespace(data, i)
		if token == key && j < len(data) && data[j] == '=' {
			valueStart := skipASCIIWhitespace(data, j+1)
			return assignmentSpan{KeyStart: start, KeyEnd: i, Equal: j, ValueStart: valueStart}, true
		}
		i--
	}
	return assignmentSpan{}, false
}

func findScalarValueEnd(data []byte, from int) (int, bool) {
	for i := from; i < len(data); i++ {
		if isLineCommentStart(data, i) {
			return trimRightASCII(data, from, i), true
		}
		if data[i] == '"' {
			next, ok := skipString(data, i)
			if !ok {
				return 0, false
			}
			i = next
			continue
		}
		switch data[i] {
		case ';', '\n', '\r':
			return trimRightASCII(data, from, i), true
		case '{', '}', '(', ')':
			return 0, false
		}
	}
	return trimRightASCII(data, from, len(data)), true
}

func skipASCIIWhitespace(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func trimRightASCII(data []byte, start, end int) int {
	for end > start {
		switch data[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			return end
		}
	}
	return end
}

func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isIdent(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isLineCommentStart(data []byte, i int) bool {
	if i >= len(data) {
		return false
	}
	if data[i] != '#' && !(data[i] == '/' && i+1 < len(data) && data[i+1] == '/') {
		return false
	}
	if i == 0 {
		return true
	}
	switch data[i-1] {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func skipLine(data []byte, i int) int {
	for i < len(data) && data[i] != '\n' {
		i++
	}
	return i
}

func skipString(data []byte, quote int) (int, bool) {
	escaped := false
	for i := quote + 1; i < len(data); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch data[i] {
		case '\\':
			escaped = true
		case '"':
			return i, true
		}
	}
	return 0, false
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	parts := strings.SplitAfter(string(data), "\n")
	if parts[len(parts)-1] == "" {
		return parts[:len(parts)-1]
	}
	return parts
}

func parseINISection(line string) (string, bool) {
	body, _ := splitEOL(line)
	x := strings.TrimSpace(body)
	if strings.HasPrefix(x, "#") || strings.HasPrefix(x, "//") || !strings.HasPrefix(x, "[") || !strings.HasSuffix(x, "]") {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(x, "["), "]"))), true
}

func isCommentLine(line string) bool {
	x := strings.TrimSpace(line)
	return strings.HasPrefix(x, "#") || strings.HasPrefix(x, "//")
}

func lineHasAssignmentKey(line, key string) bool {
	body, _ := splitEOL(line)
	x := strings.TrimSpace(body)
	if x == "" || strings.HasPrefix(x, "#") || strings.HasPrefix(x, "//") {
		return false
	}
	idx := strings.Index(x, "=")
	if idx < 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(x[:idx]), key)
}

func replaceAssignmentLine(line, key, value string, semicolon bool) string {
	body, eol := splitEOL(line)
	leadingLen := len(body) - len(strings.TrimLeft(body, " \t"))
	leading := body[:leadingLen]
	comment := ""
	if idx := strings.Index(body, "#"); idx >= 0 {
		comment = " " + strings.TrimSpace(body[idx:])
	}
	suffix := ""
	if semicolon {
		suffix = ";"
	}
	return leading + key + " = " + value + suffix + comment + eol
}

func splitEOL(line string) (body, eol string) {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	case strings.HasSuffix(line, "\n"):
		return strings.TrimSuffix(line, "\n"), "\n"
	default:
		return line, ""
	}
}

func closingIndent(data []byte, pos int) string {
	lineStart := bytes.LastIndexByte(data[:pos], '\n') + 1
	line := string(data[lineStart:pos])
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func insertLine(lines []string, idx int, line string) []string {
	if idx < 0 || idx > len(lines) {
		idx = len(lines)
	}
	lines = append(lines, "")
	copy(lines[idx+1:], lines[idx:])
	lines[idx] = line
	return lines
}

func insertBytes(data []byte, idx int, insert []byte) []byte {
	out := make([]byte, 0, len(data)+len(insert))
	out = append(out, data[:idx]...)
	out = append(out, insert...)
	out = append(out, data[idx:]...)
	return out
}

func replaceRange(data []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(data)-end+start+len(replacement))
	out = append(out, data[:start]...)
	out = append(out, replacement...)
	out = append(out, data[end:]...)
	return out
}
