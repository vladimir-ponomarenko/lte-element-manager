package srsranconf

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type ENBConfig struct {
	Serial          string
	MCC             string
	MNC             string
	NPRB            uint32
	ENBID           string
	MMEAddr         string
	GTPBindAddr     string
	S1CBindAddr     string
	S1CBindPort     uint32
	TM              uint32
	TXGain          float64
	RXGain          float64
	TimeAdvNSamples string
	DeviceName      string
	DeviceArgs      string
	BaseSrateHz     float64
	SIBConfigFile   string
	RRConfigFile    string
	RBConfigFile    string

	// Scheduler.
	SchedPolicy       string
	PDSCHMaxMCS       int32
	PUSCHMaxMCS       int32
	TargetBLER        float64
	MinNofCtrlSymbols int32
	MaxNofCtrlSymbols int32

	// Expert.
	PUSCHMaxIts        int32
	NRPUSCHMaxIts      int32
	PUSCH8bitDecoder   bool
	NofPHYThreads      int32
	MetricsPeriodSecs  int32
	TXAmplitude        float64
	RRCInactivityTimer int32
	RLFReleaseTimerMs  int32
	EEAPrefList        string
	EIAPrefList        string
	GTPUTunnelTimeout  int32
	S1SetupMaxRetries  int32
	S1ConnectTimer     int32
	RXGainOffset       float64
	UseCedronFEstAlg   bool
	RLFMinULSNREstim   float64
	MaxMacDLKOs        int32
	MaxMacULKOs        int32
	ReportJSONUDS      bool
	ReportJSONUDSPath  string
	AlarmsLogEnable    bool
	AlarmsFilename     string
}

type RRConfig struct {
	DLEARFCN      uint32
	PCI           uint32
	CellID        string
	TAC           string
	HOActive      *bool
	A3Offset      int32
	TimeToTrigger int32
	Hysteresis    int32
}

func ParseENB(path string) (ENBConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return ENBConfig{}, err
	}
	defer f.Close()

	var out ENBConfig
	section := ""

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := stripInlineComment(sc.Text())
		if line == "" {
			continue
		}
		if sec, ok := parseSectionLine(line); ok {
			section = sec
			continue
		}
		rawKey, rawVal, ok := parseAssignmentLine(line)
		if !ok {
			continue
		}
		key := strings.ToLower(rawKey)
		val := unquoteScalar(rawVal)

		switch section {
		case "enb":
			switch key {
			case "enb_id":
				out.ENBID = val
			case "mcc":
				out.MCC = val
			case "mnc":
				out.MNC = val
			case "mme_addr":
				out.MMEAddr = val
			case "gtp_bind_addr":
				out.GTPBindAddr = val
			case "s1c_bind_addr":
				out.S1CBindAddr = val
			case "s1c_bind_port":
				if uv, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
					out.S1CBindPort = uint32(uv)
				}
			case "n_prb":
				if uv, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
					out.NPRB = uint32(uv)
				}
			case "tm":
				if uv, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
					out.TM = uint32(uv)
				}
			}
		case "enb_files":
			switch key {
			case "sib_config":
				out.SIBConfigFile = val
			case "rr_config":
				out.RRConfigFile = val
			case "rb_config":
				out.RBConfigFile = val
			}
		case "rf":
			switch key {
			case "tx_gain":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.TXGain = fv
				}
			case "rx_gain":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.RXGain = fv
				}
			case "time_adv_nsamples":
				out.TimeAdvNSamples = val
			case "device_args":
				out.DeviceArgs = val
				if srate, ok := parseDeviceArgsBaseSrate(val); ok {
					out.BaseSrateHz = srate
				}
			case "device_name":
				out.DeviceName = val
			}
		case "scheduler":
			switch key {
			case "policy":
				out.SchedPolicy = val
			case "pdsch_max_mcs":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.PDSCHMaxMCS = int32(iv)
				}
			case "pusch_max_mcs":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.PUSCHMaxMCS = int32(iv)
				}
			case "target_bler":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.TargetBLER = fv
				}
			case "min_nof_ctrl_symbols":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.MinNofCtrlSymbols = int32(iv)
				}
			case "max_nof_ctrl_symbols":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.MaxNofCtrlSymbols = int32(iv)
				}
			}
		case "expert":
			switch key {
			case "enb_serial":
				out.Serial = val
			case "pusch_max_its":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.PUSCHMaxIts = int32(iv)
				}
			case "nr_pusch_max_its":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.NRPUSCHMaxIts = int32(iv)
				}
			case "pusch_8bit_decoder":
				switch strings.ToLower(val) {
				case "true":
					out.PUSCH8bitDecoder = true
				case "false":
					out.PUSCH8bitDecoder = false
				}
			case "nof_phy_threads":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.NofPHYThreads = int32(iv)
				}
			case "metrics_period_secs":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.MetricsPeriodSecs = int32(iv)
				}
			case "report_json_uds_enable":
				switch strings.ToLower(val) {
				case "true":
					out.ReportJSONUDS = true
				case "false":
					out.ReportJSONUDS = false
				}
			case "report_json_uds_path":
				out.ReportJSONUDSPath = val
			case "alarms_log_enable":
				switch strings.ToLower(val) {
				case "true":
					out.AlarmsLogEnable = true
				case "false":
					out.AlarmsLogEnable = false
				}
			case "alarms_filename":
				out.AlarmsFilename = val
			case "tx_amplitude":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.TXAmplitude = fv
				}
			case "rrc_inactivity_timer":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.RRCInactivityTimer = int32(iv)
				}
			case "rlf_release_timer_ms":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.RLFReleaseTimerMs = int32(iv)
				}
			case "eea_pref_list":
				out.EEAPrefList = val
			case "eia_pref_list":
				out.EIAPrefList = val
			case "gtpu_tunnel_timeout":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.GTPUTunnelTimeout = int32(iv)
				}
			case "s1_setup_max_retries":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.S1SetupMaxRetries = int32(iv)
				}
			case "s1_connect_timer":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.S1ConnectTimer = int32(iv)
				}
			case "rx_gain_offset":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.RXGainOffset = fv
				}
			case "use_cedron_f_est_alg":
				switch strings.ToLower(val) {
				case "true":
					out.UseCedronFEstAlg = true
				case "false":
					out.UseCedronFEstAlg = false
				}
			case "rlf_min_ul_snr_estim":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.RLFMinULSNREstim = fv
				}
			case "max_mac_dl_kos":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.MaxMacDLKOs = int32(iv)
				}
			case "max_mac_ul_kos":
				if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
					out.MaxMacULKOs = int32(iv)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return ENBConfig{}, err
	}
	if strings.TrimSpace(out.Serial) == "" {
		return ENBConfig{}, fmt.Errorf("enb_serial is not found in %s", path)
	}
	if out.NPRB == 0 {
		return ENBConfig{}, fmt.Errorf("n_prb is not found in %s", path)
	}
	return out, nil
}

func parseDeviceArgsBaseSrate(deviceArgs string) (float64, bool) {
	parts := strings.Split(deviceArgs, ",")
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if strings.TrimSpace(kv[0]) != "base_srate" {
			continue
		}
		v := strings.TrimSpace(kv[1])
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func ParseRR(path string) (RRConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return RRConfig{}, err
	}
	defer f.Close()

	var out RRConfig
	var gotEARFCN, gotPCI bool

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := stripInlineComment(sc.Text())
		if line == "" {
			continue
		}
		rawKey, rawVal, ok := parseAssignmentLine(line)
		if !ok {
			continue
		}
		key := strings.ToLower(rawKey)
		val := unquoteScalar(rawVal)

		switch key {
		case "cell_id":
			if out.CellID == "" {
				out.CellID = val
			}
		case "tac":
			if out.TAC == "" {
				out.TAC = val
			}
		case "dl_earfcn":
			if !gotEARFCN {
				if v, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
					out.DLEARFCN = uint32(v)
					gotEARFCN = true
				}
			}
		case "pci":
			if !gotPCI {
				if v, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
					out.PCI = uint32(v)
					gotPCI = true
				}
			}
		case "ho_active":
			if out.HOActive == nil {
				switch strings.ToLower(val) {
				case "true":
					v := true
					out.HOActive = &v
				case "false":
					v := false
					out.HOActive = &v
				}
			}
		case "a3_offset":
			if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
				out.A3Offset = int32(iv)
			}
		case "time_to_trigger":
			if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
				out.TimeToTrigger = int32(iv)
			}
		case "hysteresis":
			if iv, parseErr := strconv.ParseInt(val, 10, 32); parseErr == nil {
				out.Hysteresis = int32(iv)
			}
		}
		if gotEARFCN && gotPCI {
			// Keep scanning to allow collecting optional fields too.
		}
	}
	if err := sc.Err(); err != nil {
		return RRConfig{}, err
	}
	if !gotEARFCN || !gotPCI {
		return RRConfig{}, fmt.Errorf("dl_earfcn/pci are not found in %s", path)
	}
	return out, nil
}

func stripInlineComment(s string) string {
	end := len(s)
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
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
		case '#':
			if isInlineCommentAt(s, i) {
				end = i
				i = len(s)
			}
		case '/':
			if i+1 < len(s) && s[i+1] == '/' && isInlineCommentAt(s, i) {
				end = i
				i = len(s)
			}
		}
	}
	return strings.TrimSpace(s[:end])
}

func parseSectionLine(line string) (string, bool) {
	x := strings.TrimSpace(line)
	if len(x) < 3 || x[0] != '[' || x[len(x)-1] != ']' {
		return "", false
	}
	name := strings.TrimSpace(x[1 : len(x)-1])
	if name == "" {
		return "", false
	}
	return strings.ToLower(name), true
}

func parseAssignmentLine(line string) (key, value string, ok bool) {
	x := strings.TrimSpace(line)
	if x == "" || !isIdentifierStart(x[0]) {
		return "", "", false
	}
	i := 1
	for i < len(x) && isIdentifier(x[i]) {
		i++
	}
	key = strings.TrimSpace(x[:i])
	j := skipSpace(x, i)
	if key == "" || j >= len(x) || x[j] != '=' {
		return "", "", false
	}
	value = strings.TrimSpace(x[j+1:])
	if strings.HasSuffix(value, ";") {
		value = strings.TrimSpace(strings.TrimSuffix(value, ";"))
	}
	return key, value, true
}

func unquoteScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if unq, err := strconv.Unquote(v); err == nil {
			return unq
		}
		return v[1 : len(v)-1]
	}
	return v
}

func skipSpace(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func isIdentifierStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isIdentifier(c byte) bool {
	return isIdentifierStart(c) || (c >= '0' && c <= '9')
}

func isInlineCommentAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	if s[i] != '#' && !(s[i] == '/' && i+1 < len(s) && s[i+1] == '/') {
		return false
	}
	if i == 0 {
		return true
	}
	switch s[i-1] {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
