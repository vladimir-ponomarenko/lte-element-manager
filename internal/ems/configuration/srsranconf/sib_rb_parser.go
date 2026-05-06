package srsranconf

import (
	"fmt"
	"os"
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
	asn, ok := findAssignmentInText(src, anchor, 0)
	if !ok {
		return "", false
	}
	p := skipTextSpace(src, asn.valueStart)
	if p >= len(src) || src[p] != '(' {
		return "", false
	}
	end, ok := findMatchingDelimiter(src, p, '(', ')')
	if !ok {
		return "", false
	}
	return src[p : end+1], true
}

func extractTopLevelBraceObjects(section string) []string {
	// ( { ... }, { ... } );
	out := []string{}
	for i := 0; i < len(section); i++ {
		if isTextLineCommentStart(section, i) {
			i = skipTextLine(section, i)
			continue
		}
		if section[i] == '"' {
			next, ok := skipTextString(section, i)
			if !ok {
				return out
			}
			i = next
			continue
		}
		if section[i] != '{' {
			continue
		}
		end, ok := findMatchingDelimiter(section, i, '{', '}')
		if !ok {
			return out
		}
		out = append(out, section[i:end+1])
		i = end
	}
	return out
}

func extractNamedBlock(src, name string) (string, bool) {
	asn, ok := findAssignmentInText(src, name, 0)
	if !ok {
		return "", false
	}
	br := skipTextSpace(src, asn.valueStart)
	if br >= len(src) || src[br] != '{' {
		return "", false
	}
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
	end, ok := findMatchingDelimiter(src, braceIdx, '{', '}')
	if !ok {
		return "", false
	}
	return src[braceIdx : end+1], true
}

func findStringInBlock(block, key string) (string, bool) {
	asn, ok := findAssignmentInText(block, key, 0)
	if !ok {
		return "", false
	}
	valueEnd, ok := findTextScalarValueEnd(block, asn.valueStart)
	if !ok {
		return "", false
	}
	return unquoteScalar(block[asn.valueStart:valueEnd]), true
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

type textAssignment struct {
	valueStart int
}

func findAssignmentInText(src, key string, from int) (textAssignment, bool) {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(src); i++ {
		if isTextLineCommentStart(src, i) {
			i = skipTextLine(src, i)
			continue
		}
		if src[i] == '"' {
			next, ok := skipTextString(src, i)
			if !ok {
				return textAssignment{}, false
			}
			i = next
			continue
		}
		if !isIdentifierStart(src[i]) {
			continue
		}
		start := i
		for i < len(src) && isIdentifier(src[i]) {
			i++
		}
		token := src[start:i]
		j := skipTextSpace(src, i)
		if strings.EqualFold(token, key) && j < len(src) && src[j] == '=' {
			return textAssignment{valueStart: skipTextSpace(src, j+1)}, true
		}
		i--
	}
	return textAssignment{}, false
}

func findTextScalarValueEnd(src string, from int) (int, bool) {
	for i := from; i < len(src); i++ {
		if isTextLineCommentStart(src, i) {
			return trimTextRight(src, from, i), true
		}
		if src[i] == '"' {
			next, ok := skipTextString(src, i)
			if !ok {
				return 0, false
			}
			i = next
			continue
		}
		switch src[i] {
		case ';', '\n', '\r':
			return trimTextRight(src, from, i), true
		case '{', '}', '(', ')':
			return 0, false
		}
	}
	return trimTextRight(src, from, len(src)), true
}

func findMatchingDelimiter(src string, open int, openCh, closeCh byte) (int, bool) {
	depth := 0
	for i := open; i < len(src); i++ {
		if isTextLineCommentStart(src, i) {
			i = skipTextLine(src, i)
			continue
		}
		if src[i] == '"' {
			next, ok := skipTextString(src, i)
			if !ok {
				return 0, false
			}
			i = next
			continue
		}
		switch src[i] {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func skipTextSpace(src string, i int) int {
	for i < len(src) {
		switch src[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func trimTextRight(src string, start, end int) int {
	for end > start {
		switch src[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			return end
		}
	}
	return end
}

func isTextLineCommentStart(src string, i int) bool {
	if i >= len(src) {
		return false
	}
	if src[i] != '#' && !(src[i] == '/' && i+1 < len(src) && src[i+1] == '/') {
		return false
	}
	if i == 0 {
		return true
	}
	switch src[i-1] {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func skipTextLine(src string, i int) int {
	for i < len(src) && src[i] != '\n' {
		i++
	}
	return i
}

func skipTextString(src string, i int) (int, bool) {
	if i >= len(src) || src[i] != '"' {
		return i, false
	}
	escaped := false
	for i++; i < len(src); i++ {
		if escaped {
			escaped = false
			continue
		}
		if src[i] == '\\' {
			escaped = true
			continue
		}
		if src[i] == '"' {
			return i, true
		}
	}
	return 0, false
}
