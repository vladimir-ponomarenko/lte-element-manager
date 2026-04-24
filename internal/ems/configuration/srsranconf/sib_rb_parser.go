package srsranconf

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type SIBConfig struct {
	// sib1
	QRxLevMin  int32
	CellBarred string // "NotBarred" / "Barred"

	// sib2.rr_config_common_sib.rach_cnfg
	NumRAPreambles          int32
	PreambleInitRxTargetPwr int32
	PwrRampingStep          int32

	// sib2.rr_config_common_sib.pcch_cnfg
	DefaultPagingCycle int32

	// sib2.rr_config_common_sib.ul_pwr_ctrl
	P0NominalPUSCH int32
	P0NominalPUCCH int32
	Alpha          float64

	// sib2.rr_config_common_sib.pdsch_cnfg
	ReferenceSignalPower int32

	// sib2.ue_timers_and_constants
	T300 int32
	T301 int32
	T310 int32
	N310 int32
	T311 int32
}

type RBConfig struct {
	Profiles []QCIProfile
}

type QCIProfile struct {
	QCI           int32
	DiscardTimer  int32
	PDCPSNSize    int32
	TPollRetx     int32
	MaxRetxThresh int32
	TReordering   int32
	Priority      int32
}

var (
	reAnyKV = regexp.MustCompile(`(?i)([A-Za-z0-9_]+)\s*=\s*(.*?)\s*;`)
)

func ParseSIB(path string) (SIBConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SIBConfig{}, err
	}
	src := string(raw)

	sib1, ok := extractNamedBlock(src, "sib1")
	if !ok {
		return SIBConfig{}, fmt.Errorf("sib1 block is not found in %s", path)
	}
	sib2, ok := extractNamedBlock(src, "sib2")
	if !ok {
		return SIBConfig{}, fmt.Errorf("sib2 block is not found in %s", path)
	}

	var out SIBConfig
	out.QRxLevMin, _ = findInt32InBlock(sib1, "q_rx_lev_min")
	out.CellBarred, _ = findStringInBlock(sib1, "cell_barred")

	rach, _ := extractNestedBlock(sib2, "rach_cnfg")
	out.NumRAPreambles, _ = findInt32InBlock(rach, "num_ra_preambles")
	out.PreambleInitRxTargetPwr, _ = findInt32InBlock(rach, "preamble_init_rx_target_pwr")
	out.PwrRampingStep, _ = findInt32InBlock(rach, "pwr_ramping_step")

	pcch, _ := extractNestedBlock(sib2, "pcch_cnfg")
	out.DefaultPagingCycle, _ = findInt32InBlock(pcch, "default_paging_cycle")

	ulPwr, _ := extractNestedBlock(sib2, "ul_pwr_ctrl")
	out.P0NominalPUSCH, _ = findInt32InBlock(ulPwr, "p0_nominal_pusch")
	out.P0NominalPUCCH, _ = findInt32InBlock(ulPwr, "p0_nominal_pucch")
	out.Alpha, _ = findFloat64InBlock(ulPwr, "alpha")

	pdsch, _ := extractNestedBlock(sib2, "pdsch_cnfg")
	out.ReferenceSignalPower, _ = findInt32InBlock(pdsch, "rs_power")

	timers, _ := extractNestedBlock(sib2, "ue_timers_and_constants")
	out.T300, _ = findInt32InBlock(timers, "t300")
	out.T301, _ = findInt32InBlock(timers, "t301")
	out.T310, _ = findInt32InBlock(timers, "t310")
	out.N310, _ = findInt32InBlock(timers, "n310")
	out.T311, _ = findInt32InBlock(timers, "t311")

	if out.DefaultPagingCycle == 0 {
		return SIBConfig{}, fmt.Errorf("default_paging_cycle is not found in %s", path)
	}
	return out, nil
}

func ParseRB(path string) (RBConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RBConfig{}, err
	}
	src := string(raw)

	section, ok := extractParenSectionAfter(src, "qci_config")
	if !ok {
		return RBConfig{}, fmt.Errorf("qci_config is not found in %s", path)
	}
	objs := extractTopLevelBraceObjects(section)
	if len(objs) == 0 {
		return RBConfig{}, fmt.Errorf("qci_config has no entries in %s", path)
	}

	var out RBConfig
	out.Profiles = make([]QCIProfile, 0, len(objs))
	for _, obj := range objs {
		var p QCIProfile
		p.QCI, _ = findInt32InBlock(obj, "qci")
		if p.QCI == 0 {
			continue
		}
		p.DiscardTimer, _ = findInt32InBlock(obj, "discard_timer")
		p.PDCPSNSize, _ = findInt32InBlock(obj, "pdcp_sn_size")
		p.TPollRetx, _ = findInt32InBlock(obj, "t_poll_retx")
		p.MaxRetxThresh, _ = findInt32InBlock(obj, "max_retx_thresh")
		p.TReordering, _ = findInt32InBlock(obj, "t_reordering")
		p.Priority, _ = findInt32InBlock(obj, "priority")
		out.Profiles = append(out.Profiles, p)
	}
	if len(out.Profiles) == 0 {
		return RBConfig{}, fmt.Errorf("no qci entries parsed in %s", path)
	}
	return out, nil
}

func extractParenSectionAfter(src, anchor string) (string, bool) {
	idx := strings.Index(src, anchor)
	if idx < 0 {
		return "", false
	}
	p := strings.Index(src[idx:], "(")
	if p < 0 {
		return "", false
	}
	p = idx + p
	depth := 0
	for i := p; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[p : i+1], true
			}
		}
	}
	return "", false
}

func extractTopLevelBraceObjects(section string) []string {
	// ( { ... }, { ... } );
	out := []string{}
	// Find first '{'
	start := strings.Index(section, "{")
	if start < 0 {
		return out
	}
	i := start
	depth := 0
	objStart := -1
	for i < len(section) {
		switch section[i] {
		case '{':
			depth++
			if depth == 1 {
				objStart = i
			}
		case '}':
			if depth == 1 && objStart >= 0 {
				out = append(out, section[objStart:i+1])
				objStart = -1
			}
			if depth > 0 {
				depth--
			}
		}
		i++
	}
	return out
}

func extractNamedBlock(src, name string) (string, bool) {
	// Matches: name = { ... };
	idx := strings.Index(src, name)
	if idx < 0 {
		return "", false
	}
	br := strings.Index(src[idx:], "{")
	if br < 0 {
		return "", false
	}
	br = idx + br
	block, ok := extractBraceBlock(src, br)
	return block, ok
}

func extractNestedBlock(src, name string) (string, bool) {
	return extractNamedBlock(src, name)
}

func extractBraceBlock(src string, braceIdx int) (string, bool) {
	if braceIdx < 0 || braceIdx >= len(src) || src[braceIdx] != '{' {
		return "", false
	}
	depth := 0
	for i := braceIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[braceIdx : i+1], true
			}
		}
	}
	return "", false
}

func findStringInBlock(block, key string) (string, bool) {
	for _, m := range reAnyKV.FindAllStringSubmatch(block, -1) {
		if len(m) != 3 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(m[1]), key) {
			v := strings.TrimSpace(m[2])
			v = strings.Trim(v, `"`)
			return v, true
		}
	}

	reLoose := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(key) + `\b\s*=\s*([^;\n}]+)`)
	if m := reLoose.FindStringSubmatch(block); len(m) == 2 {
		v := strings.TrimSpace(m[1])
		v = strings.Trim(v, `"`)
		return v, true
	}
	return "", false
}

func findInt32InBlock(block, key string) (int32, bool) {
	s, ok := findStringInBlock(block, key)
	if !ok {
		return 0, false
	}
	iv, err := strconv.ParseInt(s, 10, 32)
	return int32(iv), err == nil
}

func findFloat64InBlock(block, key string) (float64, bool) {
	s, ok := findStringInBlock(block, key)
	if !ok {
		return 0, false
	}
	fv, err := strconv.ParseFloat(s, 64)
	return fv, err == nil
}
