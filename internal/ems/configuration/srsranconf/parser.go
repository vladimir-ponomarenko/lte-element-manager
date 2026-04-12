package srsranconf

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type ENBConfig struct {
	Serial string
	MCC    string
	MNC    string
	NPRB   uint32
	TXGain float64
	BaseSrateHz float64
}

type RRConfig struct {
	DLEARFCN uint32
	PCI      uint32
}

var (
	reSection = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	reKV      = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*(.*?)\s*;?\s*$`)
)

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
		if m := reSection.FindStringSubmatch(line); len(m) == 2 {
			section = strings.ToLower(strings.TrimSpace(m[1]))
			continue
		}
		m := reKV.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.Trim(strings.TrimSpace(m[2]), `"`)

		switch section {
		case "enb":
			switch key {
			case "mcc":
				out.MCC = val
			case "mnc":
				out.MNC = val
			case "n_prb":
				if uv, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
					out.NPRB = uint32(uv)
				}
			}
		case "rf":
			switch key {
			case "tx_gain":
				if fv, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
					out.TXGain = fv
				}
			case "device_args":
				if srate, ok := parseDeviceArgsBaseSrate(val); ok {
					out.BaseSrateHz = srate
				}
			}
		case "expert":
			if key == "enb_serial" {
				out.Serial = val
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
		m := reKV.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.Trim(strings.TrimSpace(m[2]), `"`)

		switch key {
		case "dl_earfcn":
			if v, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
				out.DLEARFCN = uint32(v)
				gotEARFCN = true
			}
		case "pci":
			if v, parseErr := strconv.ParseUint(val, 10, 32); parseErr == nil {
				out.PCI = uint32(v)
				gotPCI = true
			}
		}
		if gotEARFCN && gotPCI {
			return out, nil
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
	x := strings.TrimSpace(s)
	if x == "" {
		return ""
	}
	if strings.HasPrefix(x, "#") || strings.HasPrefix(x, "//") {
		return ""
	}
	if i := strings.Index(x, "//"); i >= 0 {
		x = strings.TrimSpace(x[:i])
	}
	if i := strings.Index(x, "#"); i >= 0 {
		x = strings.TrimSpace(x[:i])
	}
	return x
}
