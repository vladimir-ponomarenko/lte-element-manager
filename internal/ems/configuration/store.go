package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"lte-element-manager/internal/ems/configuration/srsranconf"
)

type EditableConfig struct {
	ENBSerial string  `json:"enb_serial"`
	MCC       string  `json:"mcc"`
	MNC       string  `json:"mnc"`
	NPRB      uint32  `json:"n_prb"`
	TXGain    float64 `json:"tx_gain"`
	DLEARFCN  uint32  `json:"dl_earfcn"`
	PCI       uint32  `json:"pci"`
}

type Store struct {
	mu      sync.RWMutex
	running EditableConfig
	cand    EditableConfig
	enbPath string
	rrPath  string
}

func NewStore(enbPath, rrPath string) (*Store, error) {
	enb, err := srsranconf.ParseENB(enbPath)
	if err != nil {
		return nil, err
	}
	rr, err := srsranconf.ParseRR(rrPath)
	if err != nil {
		return nil, err
	}
	cfg := EditableConfig{
		ENBSerial: strings.TrimSpace(enb.Serial),
		MCC:       strings.TrimSpace(enb.MCC),
		MNC:       strings.TrimSpace(enb.MNC),
		NPRB:      enb.NPRB,
		TXGain:    enb.TXGain,
		DLEARFCN:  rr.DLEARFCN,
		PCI:       rr.PCI,
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return &Store{
		running: cfg,
		cand:    cfg,
		enbPath: enbPath,
		rrPath:  rrPath,
	}, nil
}

func (s *Store) Running() EditableConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Store) Candidate() EditableConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cand
}

func (s *Store) Edit(changes map[string]any) (EditableConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.cand
	for k, v := range changes {
		if err := applyChange(&next, strings.TrimSpace(k), v); err != nil {
			return EditableConfig{}, err
		}
	}
	if err := validate(next); err != nil {
		return EditableConfig{}, err
	}
	s.cand = next
	return next, nil
}

func (s *Store) ResetCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cand = s.running
}

func (s *Store) Commit() (EditableConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validate(s.cand); err != nil {
		return EditableConfig{}, err
	}
	if err := writeENBConfig(s.enbPath, s.cand); err != nil {
		return EditableConfig{}, err
	}
	if err := writeRRConfig(s.rrPath, s.cand); err != nil {
		return EditableConfig{}, err
	}
	s.running = s.cand
	return s.running, nil
}

func applyChange(cfg *EditableConfig, key string, val any) error {
	switch key {
	case "enb_serial":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("enb_serial must be string")
		}
		cfg.ENBSerial = strings.TrimSpace(s)
	case "mcc":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("mcc must be string")
		}
		cfg.MCC = strings.TrimSpace(s)
	case "mnc":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("mnc must be string")
		}
		cfg.MNC = strings.TrimSpace(s)
	case "tx_gain":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("tx_gain must be number")
		}
		cfg.TXGain = f
	case "n_prb":
		u, ok := asUint32(val)
		if !ok {
			return fmt.Errorf("n_prb must be uint32")
		}
		cfg.NPRB = u
	case "dl_earfcn":
		u, ok := asUint32(val)
		if !ok {
			return fmt.Errorf("dl_earfcn must be uint32")
		}
		cfg.DLEARFCN = u
	case "pci":
		u, ok := asUint32(val)
		if !ok {
			return fmt.Errorf("pci must be uint32")
		}
		cfg.PCI = u
	default:
		return fmt.Errorf("unsupported config key: %s", key)
	}
	return nil
}

func validate(cfg EditableConfig) error {
	if strings.TrimSpace(cfg.ENBSerial) == "" || len(cfg.ENBSerial) > 128 {
		return fmt.Errorf("enb_serial is invalid")
	}
	if ok, _ := regexp.MatchString(`^[0-9]{3}$`, cfg.MCC); !ok {
		return fmt.Errorf("mcc must match [0-9]{3}")
	}
	if ok, _ := regexp.MatchString(`^[0-9]{2,3}$`, cfg.MNC); !ok {
		return fmt.Errorf("mnc must match [0-9]{2,3}")
	}
	if cfg.PCI > 503 {
		return fmt.Errorf("pci must be in range 0..503")
	}
	if cfg.DLEARFCN > 262143 {
		return fmt.Errorf("dl_earfcn must be in range 0..262143")
	}
	if cfg.TXGain < 0 || cfg.TXGain > 120 {
		return fmt.Errorf("tx_gain must be in range 0..120")
	}
	switch cfg.NPRB {
	case 6, 15, 25, 50, 75, 100:
	default:
		return fmt.Errorf("n_prb must be one of 6,15,25,50,75,100")
	}
	return nil
}

func writeENBConfig(path string, cfg EditableConfig) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(b)
	src = replaceInSection(src, "enb", "mcc", cfg.MCC)
	src = replaceInSection(src, "enb", "mnc", cfg.MNC)
	src = replaceInSection(src, "enb", "n_prb", strconv.FormatUint(uint64(cfg.NPRB), 10))
	src = replaceInSection(src, "rf", "tx_gain", trimFloat(cfg.TXGain))
	src = replaceInSection(src, "expert", "enb_serial", cfg.ENBSerial)
	return atomicWrite(path, []byte(src))
}

func writeRRConfig(path string, cfg EditableConfig) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(b)
	src = replaceFirstKey(src, "dl_earfcn", strconv.FormatUint(uint64(cfg.DLEARFCN), 10))
	src = replaceFirstKey(src, "pci", strconv.FormatUint(uint64(cfg.PCI), 10))
	return atomicWrite(path, []byte(src))
}

func replaceInSection(src, section, key, value string) string {
	lines := strings.Split(src, "\n")
	current := ""
	reSection := regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	reKey := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)([^#;]*)(.*)$`)
	for i, line := range lines {
		if m := reSection.FindStringSubmatch(line); len(m) == 2 {
			current = strings.ToLower(strings.TrimSpace(m[1]))
			continue
		}
		if current != strings.ToLower(section) {
			continue
		}
		if m := reKey.FindStringSubmatch(line); len(m) == 4 {
			lines[i] = m[1] + value + m[3]
			return strings.Join(lines, "\n")
		}
	}
	return src
}

func replaceFirstKey(src, key, value string) string {
	lines := strings.Split(src, "\n")
	reKey := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)([^;#]*)(.*)$`)
	for i, line := range lines {
		if m := reKey.FindStringSubmatch(line); len(m) == 4 {
			lines[i] = m[1] + value + m[3]
			return strings.Join(lines, "\n")
		}
	}
	return src
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".cfg-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		if errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EPERM) {
			return writeInPlace(path, data)
		}
		return err
	}
	return nil
}

func writeInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

func asString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	default:
		return "", false
	}
}

func asFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return x, true
	case float32:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func asUint32(v any) (uint32, bool) {
	switch x := v.(type) {
	case float64:
		if x < 0 || x > math.MaxUint32 || math.Trunc(x) != x {
			return 0, false
		}
		return uint32(x), true
	case int:
		if x < 0 {
			return 0, false
		}
		return uint32(x), true
	case int64:
		if x < 0 || x > math.MaxUint32 {
			return 0, false
		}
		return uint32(x), true
	case uint32:
		return x, true
	case json.Number:
		u, err := strconv.ParseUint(string(x), 10, 32)
		return uint32(u), err == nil
	default:
		return 0, false
	}
}

func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}
