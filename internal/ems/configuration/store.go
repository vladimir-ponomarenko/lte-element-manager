package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
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
	// Element identity.
	ENBSerial string `json:"enb_serial"`

	// enb.conf [enb]
	ENBID       string `json:"enb_id"`
	MCC         string `json:"mcc"`
	MNC         string `json:"mnc"`
	MMEAddr     string `json:"mme_addr"`
	GTPBindAddr string `json:"gtp_bind_addr"`
	S1CBindAddr string `json:"s1c_bind_addr"`
	S1CBindPort uint32 `json:"s1c_bind_port"`
	NPRB        uint32 `json:"n_prb"`
	TM          uint32 `json:"tm"`

	// enb.conf [rf]
	DeviceName      string  `json:"device_name"`
	DeviceArgs      string  `json:"device_args"`
	TXGain          float64 `json:"tx_gain"`
	RXGain          float64 `json:"rx_gain"`
	TimeAdvNSamples string  `json:"time_adv_nsamples"`

	// enb.conf [scheduler]
	SchedPolicy    string  `json:"sched_policy"`
	PDSCHMaxMCS    int32   `json:"pdsch_max_mcs"`
	PUSCHMaxMCS    int32   `json:"pusch_max_mcs"`
	TargetBLER     float64 `json:"target_bler"`
	MinCtrlSymbols int32   `json:"min_nof_ctrl_symbols"`
	MaxCtrlSymbols int32   `json:"max_nof_ctrl_symbols"`

	// rr.conf (first cell in cell_list)
	CellID        string `json:"cell_id"`
	TAC           string `json:"tac"`
	DLEARFCN      uint32 `json:"dl_earfcn"`
	PCI           uint32 `json:"pci"`
	HOActive      bool   `json:"ho_active"`
	A3Offset      int32  `json:"a3_offset"`
	TimeToTrigger int32  `json:"time_to_trigger"`
	Hysteresis    int32  `json:"hysteresis"`

	// sib.conf (selected sib1/sib2)
	QRxLevMin               int32   `json:"q_rx_lev_min"`
	CellBarred              string  `json:"cell_barred"`
	NumRAPreambles          int32   `json:"num_ra_preambles"`
	PreambleInitRxTargetPwr int32   `json:"preamble_init_rx_target_pwr"`
	PwrRampingStep          int32   `json:"pwr_ramping_step"`
	ReferenceSignalPower    int32   `json:"reference_signal_power"`
	P0NominalPUSCH          int32   `json:"p0_nominal_pusch"`
	P0NominalPUCCH          int32   `json:"p0_nominal_pucch"`
	Alpha                   float64 `json:"alpha"`
	DefaultPagingCycle      int32   `json:"default_paging_cycle"`

	T300 int32 `json:"t300"`
	T301 int32 `json:"t301"`
	T310 int32 `json:"t310"`
	N310 int32 `json:"n310"`
	T311 int32 `json:"t311"`

	// rb.conf
	QCIProfiles []QCIProfile `json:"qci_profiles"`

	// enb.conf [expert]
	PUSCHMaxIts        int32   `json:"pusch_max_its"`
	NRPUSCHMaxIts      int32   `json:"nr_pusch_max_its"`
	PUSCH8bitDecoder   bool    `json:"pusch_8bit_decoder"`
	NofPHYThreads      int32   `json:"nof_phy_threads"`
	MetricsPeriodSecs  int32   `json:"metrics_period_secs"`
	TXAmplitude        float64 `json:"tx_amplitude"`
	RRCInactivityTimer int32   `json:"rrc_inactivity_timer"`
	RLFReleaseTimerMs  int32   `json:"rlf_release_timer_ms"`
	EEAPrefList        string  `json:"eea_pref_list"`
	EIAPrefList        string  `json:"eia_pref_list"`
	GTPUTunnelTimeout  int32   `json:"gtpu_tunnel_timeout"`
	S1SetupMaxRetries  int32   `json:"s1_setup_max_retries"`
	S1ConnectTimer     int32   `json:"s1_connect_timer"`
	RXGainOffset       float64 `json:"rx_gain_offset"`
	UseCedronFEstAlg   bool    `json:"use_cedron_f_est_alg"`
	RLFMinULSNREstim   float64 `json:"rlf_min_ul_snr_estim"`
	MaxMacDLKOs        int32   `json:"max_mac_dl_kos"`
	MaxMacULKOs        int32   `json:"max_mac_ul_kos"`
}

type QCIProfile struct {
	QCI           int32 `json:"qci"`
	DiscardTimer  int32 `json:"discard_timer"`
	PDCPSNSize    int32 `json:"pdcp_sn_size"`
	TPollRetx     int32 `json:"t_poll_retx"`
	MaxRetxThresh int32 `json:"max_retx_thresh"`
	TReordering   int32 `json:"t_reordering"`
	Priority      int32 `json:"priority"`
}

type Store struct {
	mu      sync.RWMutex
	running EditableConfig
	cand    EditableConfig
	enbPath string
	rrPath  string
	sibPath string
	rbPath  string

	// Tracks which underlying config files need to be regenerated on commit.
	// This prevents unrelated files from being touched when only one leaf is edited.
	dirty struct {
		enb bool
		rr  bool
		sib bool
		rb  bool
	}

	dirtyKeys map[string]struct{}
}

type FileBackup struct {
	ENB []byte
	RR  []byte
	SIB []byte
	RB  []byte
}

func NewStore(enbPath, rrPath, sibPath, rbPath string) (*Store, error) {
	enb, err := srsranconf.ParseENB(enbPath)
	if err != nil {
		return nil, err
	}
	rr, err := srsranconf.ParseRR(rrPath)
	if err != nil {
		return nil, err
	}
	sib, err := srsranconf.ParseSIB(sibPath)
	if err != nil {
		return nil, err
	}
	rb, err := srsranconf.ParseRB(rbPath)
	if err != nil {
		return nil, err
	}
	cfg := EditableConfig{
		ENBSerial: strings.TrimSpace(enb.Serial),

		ENBID:       strings.TrimSpace(enb.ENBID),
		MCC:         strings.TrimSpace(enb.MCC),
		MNC:         strings.TrimSpace(enb.MNC),
		MMEAddr:     strings.TrimSpace(enb.MMEAddr),
		GTPBindAddr: strings.TrimSpace(enb.GTPBindAddr),
		S1CBindAddr: strings.TrimSpace(enb.S1CBindAddr),
		S1CBindPort: enb.S1CBindPort,
		NPRB:        enb.NPRB,
		TM:          enb.TM,

		TXGain:          enb.TXGain,
		RXGain:          enb.RXGain,
		TimeAdvNSamples: strings.TrimSpace(enb.TimeAdvNSamples),
		DeviceName:      strings.TrimSpace(enb.DeviceName),
		DeviceArgs:      strings.TrimSpace(enb.DeviceArgs),

		SchedPolicy:    strings.TrimSpace(enb.SchedPolicy),
		PDSCHMaxMCS:    enb.PDSCHMaxMCS,
		PUSCHMaxMCS:    enb.PUSCHMaxMCS,
		TargetBLER:     enb.TargetBLER,
		MinCtrlSymbols: enb.MinNofCtrlSymbols,
		MaxCtrlSymbols: enb.MaxNofCtrlSymbols,

		CellID:        strings.TrimSpace(rr.CellID),
		TAC:           strings.TrimSpace(rr.TAC),
		DLEARFCN:      rr.DLEARFCN,
		PCI:           rr.PCI,
		HOActive:      rr.HOActive != nil && *rr.HOActive,
		A3Offset:      rr.A3Offset,
		TimeToTrigger: rr.TimeToTrigger,
		Hysteresis:    rr.Hysteresis,

		QRxLevMin:               sib.QRxLevMin,
		CellBarred:              strings.TrimSpace(sib.CellBarred),
		NumRAPreambles:          sib.NumRAPreambles,
		PreambleInitRxTargetPwr: sib.PreambleInitRxTargetPwr,
		PwrRampingStep:          sib.PwrRampingStep,
		ReferenceSignalPower:    sib.ReferenceSignalPower,
		P0NominalPUSCH:          sib.P0NominalPUSCH,
		P0NominalPUCCH:          sib.P0NominalPUCCH,
		Alpha:                   sib.Alpha,
		DefaultPagingCycle:      sib.DefaultPagingCycle,
		T300:                    sib.T300,
		T301:                    sib.T301,
		T310:                    sib.T310,
		N310:                    sib.N310,
		T311:                    sib.T311,

		QCIProfiles: convertQCIProfiles(rb.Profiles),

		PUSCHMaxIts:        enb.PUSCHMaxIts,
		NRPUSCHMaxIts:      enb.NRPUSCHMaxIts,
		PUSCH8bitDecoder:   enb.PUSCH8bitDecoder,
		NofPHYThreads:      enb.NofPHYThreads,
		MetricsPeriodSecs:  enb.MetricsPeriodSecs,
		TXAmplitude:        enb.TXAmplitude,
		RRCInactivityTimer: enb.RRCInactivityTimer,
		RLFReleaseTimerMs:  enb.RLFReleaseTimerMs,
		EEAPrefList:        strings.TrimSpace(enb.EEAPrefList),
		EIAPrefList:        strings.TrimSpace(enb.EIAPrefList),
		GTPUTunnelTimeout:  enb.GTPUTunnelTimeout,
		S1SetupMaxRetries:  enb.S1SetupMaxRetries,
		S1ConnectTimer:     enb.S1ConnectTimer,
		RXGainOffset:       enb.RXGainOffset,
		UseCedronFEstAlg:   enb.UseCedronFEstAlg,
		RLFMinULSNREstim:   enb.RLFMinULSNREstim,
		MaxMacDLKOs:        enb.MaxMacDLKOs,
		MaxMacULKOs:        enb.MaxMacULKOs,
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return &Store{
		running: cfg,
		cand:    cfg,
		enbPath: enbPath,
		rrPath:  rrPath,
		sibPath: sibPath,
		rbPath:  rbPath,
	}, nil
}

func convertQCIProfiles(in []srsranconf.QCIProfile) []QCIProfile {
	if len(in) == 0 {
		return nil
	}
	out := make([]QCIProfile, 0, len(in))
	for _, p := range in {
		out = append(out, QCIProfile{
			QCI:           p.QCI,
			DiscardTimer:  p.DiscardTimer,
			PDCPSNSize:    p.PDCPSNSize,
			TPollRetx:     p.TPollRetx,
			MaxRetxThresh: p.MaxRetxThresh,
			TReordering:   p.TReordering,
			Priority:      p.Priority,
		})
	}
	return out
}

func cloneEditableConfig(in EditableConfig) EditableConfig {
	out := in
	if len(in.QCIProfiles) > 0 {
		out.QCIProfiles = append([]QCIProfile(nil), in.QCIProfiles...)
	}
	return out
}

func (s *Store) Running() EditableConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEditableConfig(s.running)
}

func (s *Store) Candidate() EditableConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEditableConfig(s.cand)
}

func (s *Store) Edit(changes map[string]any) (EditableConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.editLocked(changes)
}

func (s *Store) PreviewEdit(changes map[string]any) (EditableConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	next := cloneEditableConfig(s.cand)
	for k, v := range changes {
		key := strings.TrimSpace(k)
		if err := applyChange(&next, key, v); err != nil {
			return EditableConfig{}, err
		}
	}
	if err := validate(next); err != nil {
		return EditableConfig{}, err
	}
	return cloneEditableConfig(next), nil
}

func ValidateConfig(cfg EditableConfig) error {
	return validate(cloneEditableConfig(cfg))
}

func (s *Store) editLocked(changes map[string]any) (EditableConfig, error) {

	if s.dirtyKeys == nil {
		s.dirtyKeys = make(map[string]struct{}, len(changes))
	}
	next := cloneEditableConfig(s.cand)
	for k, v := range changes {
		key := strings.TrimSpace(k)
		if err := applyChange(&next, key, v); err != nil {
			return EditableConfig{}, err
		}
		s.markDirtyLocked(key)
		s.dirtyKeys[key] = struct{}{}
	}
	if err := validate(next); err != nil {
		return EditableConfig{}, err
	}
	s.cand = cloneEditableConfig(next)
	return cloneEditableConfig(next), nil
}

func (s *Store) ResetCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cand = cloneEditableConfig(s.running)
	s.dirty.enb = false
	s.dirty.rr = false
	s.dirty.sib = false
	s.dirty.rb = false
	s.dirtyKeys = nil
}

func (s *Store) Commit() (EditableConfig, error) {
	backup, next, err := s.PersistCandidate()
	if err != nil {
		return EditableConfig{}, err
	}
	if err := s.FinalizeCommit(); err != nil {
		_ = s.RollbackPersist(backup)
		return EditableConfig{}, err
	}
	return next, nil
}

func (s *Store) PersistCandidate() (*FileBackup, EditableConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validate(s.cand); err != nil {
		return nil, EditableConfig{}, err
	}

	backup, err := s.backupDirtyFilesLocked()
	if err != nil {
		return nil, EditableConfig{}, err
	}

	restored := false
	restoreOnError := func() {
		if restored {
			return
		}
		restored = true
		_ = s.restoreDirtyFilesLocked(backup)
	}

	if s.dirty.enb {
		if err := writeENBConfig(s.enbPath, s.cand, s.dirtyKeys); err != nil {
			restoreOnError()
			return nil, EditableConfig{}, err
		}
	}
	if s.dirty.rr {
		if err := writeRRConfig(s.rrPath, s.cand, s.dirtyKeys); err != nil {
			restoreOnError()
			return nil, EditableConfig{}, err
		}
	}
	if s.dirty.sib {
		if err := writeSIBConfig(s.sibPath, s.cand, s.dirtyKeys); err != nil {
			restoreOnError()
			return nil, EditableConfig{}, err
		}
	}
	if s.dirty.rb {
		if err := writeRBConfig(s.rbPath, s.cand, s.dirtyKeys); err != nil {
			restoreOnError()
			return nil, EditableConfig{}, err
		}
	}

	return backup, cloneEditableConfig(s.cand), nil
}

func (s *Store) FinalizeCommit() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validate(s.cand); err != nil {
		return err
	}
	s.running = cloneEditableConfig(s.cand)
	s.dirty.enb = false
	s.dirty.rr = false
	s.dirty.sib = false
	s.dirty.rb = false
	s.dirtyKeys = nil
	return nil
}

func (s *Store) RollbackPersist(backup *FileBackup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restoreDirtyFilesLocked(backup)
}

func (s *Store) backupDirtyFilesLocked() (*FileBackup, error) {
	backup := &FileBackup{}
	var err error
	if s.dirty.enb {
		backup.ENB, err = os.ReadFile(s.enbPath)
		if err != nil {
			return nil, err
		}
	}
	if s.dirty.rr {
		backup.RR, err = os.ReadFile(s.rrPath)
		if err != nil {
			return nil, err
		}
	}
	if s.dirty.sib {
		backup.SIB, err = os.ReadFile(s.sibPath)
		if err != nil {
			return nil, err
		}
	}
	if s.dirty.rb {
		backup.RB, err = os.ReadFile(s.rbPath)
		if err != nil {
			return nil, err
		}
	}
	return backup, nil
}

func (s *Store) restoreDirtyFilesLocked(backup *FileBackup) error {
	if backup == nil {
		return nil
	}
	if s.dirty.enb && backup.ENB != nil {
		if err := atomicWrite(s.enbPath, backup.ENB); err != nil {
			return err
		}
	}
	if s.dirty.rr && backup.RR != nil {
		if err := atomicWrite(s.rrPath, backup.RR); err != nil {
			return err
		}
	}
	if s.dirty.sib && backup.SIB != nil {
		if err := atomicWrite(s.sibPath, backup.SIB); err != nil {
			return err
		}
	}
	if s.dirty.rb && backup.RB != nil {
		if err := atomicWrite(s.rbPath, backup.RB); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) markDirtyLocked(key string) {
	switch {
	// rb.conf
	case strings.HasPrefix(key, "qci_profiles["):
		s.dirty.rb = true
		return
	}
	switch key {
	// enb.conf
	case "enb_serial", "enb_id", "mcc", "mnc", "mme_addr", "gtp_bind_addr", "s1c_bind_addr", "s1c_bind_port", "n_prb", "tm",
		"tx_gain", "rx_gain", "time_adv_nsamples", "device_name", "device_args",
		"sched_policy", "pdsch_max_mcs", "pusch_max_mcs", "target_bler", "min_nof_ctrl_symbols", "max_nof_ctrl_symbols",
		"pusch_max_its", "nr_pusch_max_its", "pusch_8bit_decoder", "nof_phy_threads", "metrics_period_secs", "tx_amplitude",
		"rrc_inactivity_timer", "rlf_release_timer_ms", "eea_pref_list", "eia_pref_list", "gtpu_tunnel_timeout", "s1_setup_max_retries",
		"s1_connect_timer", "rx_gain_offset", "use_cedron_f_est_alg", "rlf_min_ul_snr_estim", "max_mac_dl_kos", "max_mac_ul_kos":
		s.dirty.enb = true

	// rr.conf
	case "cell_id", "tac", "dl_earfcn", "pci", "ho_active", "a3_offset", "time_to_trigger", "hysteresis":
		s.dirty.rr = true

	// sib.conf
	case "q_rx_lev_min", "cell_barred", "num_ra_preambles", "preamble_init_rx_target_pwr", "pwr_ramping_step",
		"reference_signal_power", "p0_nominal_pusch", "p0_nominal_pucch", "alpha", "default_paging_cycle",
		"t300", "t301", "t310", "n310", "t311":
		s.dirty.sib = true
	}
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
	case "enb_id":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("enb_id must be string")
		}
		cfg.ENBID = strings.TrimSpace(s)
	case "mme_addr":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("mme_addr must be string")
		}
		cfg.MMEAddr = strings.TrimSpace(s)
	case "gtp_bind_addr":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("gtp_bind_addr must be string")
		}
		cfg.GTPBindAddr = strings.TrimSpace(s)
	case "s1c_bind_addr":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("s1c_bind_addr must be string")
		}
		cfg.S1CBindAddr = strings.TrimSpace(s)
	case "s1c_bind_port":
		u, ok := asUint32(val)
		if !ok {
			return fmt.Errorf("s1c_bind_port must be uint32")
		}
		cfg.S1CBindPort = u
	case "tx_gain":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("tx_gain must be number")
		}
		cfg.TXGain = f
	case "rx_gain":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("rx_gain must be number")
		}
		cfg.RXGain = f
	case "device_name":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("device_name must be string")
		}
		cfg.DeviceName = strings.TrimSpace(s)
	case "device_args":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("device_args must be string")
		}
		cfg.DeviceArgs = strings.TrimSpace(s)
	case "time_adv_nsamples":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("time_adv_nsamples must be string")
		}
		cfg.TimeAdvNSamples = strings.TrimSpace(s)
	case "n_prb":
		u, ok := asUint32(val)
		if !ok {
			return fmt.Errorf("n_prb must be uint32")
		}
		cfg.NPRB = u
	case "tm":
		u, ok := asUint32(val)
		if !ok {
			return fmt.Errorf("tm must be uint32")
		}
		cfg.TM = u
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
	case "cell_id":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("cell_id must be string")
		}
		cfg.CellID = strings.TrimSpace(s)
	case "tac":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("tac must be string")
		}
		cfg.TAC = strings.TrimSpace(s)
	case "ho_active":
		switch x := val.(type) {
		case bool:
			cfg.HOActive = x
		default:
			return fmt.Errorf("ho_active must be boolean")
		}
	case "a3_offset":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("a3_offset must be int32")
		}
		cfg.A3Offset = i
	case "time_to_trigger":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("time_to_trigger must be int32")
		}
		cfg.TimeToTrigger = i
	case "hysteresis":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("hysteresis must be int32")
		}
		cfg.Hysteresis = i

	case "sched_policy":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("sched_policy must be string")
		}
		cfg.SchedPolicy = strings.TrimSpace(s)
	case "pdsch_max_mcs":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("pdsch_max_mcs must be int32")
		}
		cfg.PDSCHMaxMCS = i
	case "pusch_max_mcs":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("pusch_max_mcs must be int32")
		}
		cfg.PUSCHMaxMCS = i
	case "target_bler":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("target_bler must be number")
		}
		cfg.TargetBLER = f
	case "min_nof_ctrl_symbols":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("min_nof_ctrl_symbols must be int32")
		}
		cfg.MinCtrlSymbols = i
	case "max_nof_ctrl_symbols":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("max_nof_ctrl_symbols must be int32")
		}
		cfg.MaxCtrlSymbols = i

	case "q_rx_lev_min":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("q_rx_lev_min must be int32")
		}
		cfg.QRxLevMin = i
	case "cell_barred":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("cell_barred must be string")
		}
		cfg.CellBarred = strings.TrimSpace(s)
	case "num_ra_preambles":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("num_ra_preambles must be int32")
		}
		cfg.NumRAPreambles = i
	case "preamble_init_rx_target_pwr":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("preamble_init_rx_target_pwr must be int32")
		}
		cfg.PreambleInitRxTargetPwr = i
	case "pwr_ramping_step":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("pwr_ramping_step must be int32")
		}
		cfg.PwrRampingStep = i
	case "reference_signal_power":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("reference_signal_power must be int32")
		}
		cfg.ReferenceSignalPower = i
	case "p0_nominal_pusch":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("p0_nominal_pusch must be int32")
		}
		cfg.P0NominalPUSCH = i
	case "p0_nominal_pucch":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("p0_nominal_pucch must be int32")
		}
		cfg.P0NominalPUCCH = i
	case "alpha":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("alpha must be number")
		}
		cfg.Alpha = f
	case "default_paging_cycle":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("default_paging_cycle must be int32")
		}
		cfg.DefaultPagingCycle = i
	case "t300":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("t300 must be int32")
		}
		cfg.T300 = i
	case "t301":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("t301 must be int32")
		}
		cfg.T301 = i
	case "t310":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("t310 must be int32")
		}
		cfg.T310 = i
	case "n310":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("n310 must be int32")
		}
		cfg.N310 = i
	case "t311":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("t311 must be int32")
		}
		cfg.T311 = i

	default:
		// List edits: qci_profiles[7].discard_timer
		if strings.HasPrefix(key, "qci_profiles[") {
			return applyQCIProfileChange(cfg, key, val)
		}
		return fmt.Errorf("unsupported config key: %s", key)

	case "pusch_max_its":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("pusch_max_its must be int32")
		}
		cfg.PUSCHMaxIts = i
	case "nr_pusch_max_its":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("nr_pusch_max_its must be int32")
		}
		cfg.NRPUSCHMaxIts = i
	case "pusch_8bit_decoder":
		switch x := val.(type) {
		case bool:
			cfg.PUSCH8bitDecoder = x
		default:
			return fmt.Errorf("pusch_8bit_decoder must be boolean")
		}
	case "nof_phy_threads":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("nof_phy_threads must be int32")
		}
		cfg.NofPHYThreads = i
	case "metrics_period_secs":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("metrics_period_secs must be int32")
		}
		cfg.MetricsPeriodSecs = i
	case "tx_amplitude":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("tx_amplitude must be number")
		}
		cfg.TXAmplitude = f
	case "rrc_inactivity_timer":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("rrc_inactivity_timer must be int32")
		}
		cfg.RRCInactivityTimer = i
	case "rlf_release_timer_ms":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("rlf_release_timer_ms must be int32")
		}
		cfg.RLFReleaseTimerMs = i
	case "eea_pref_list":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("eea_pref_list must be string")
		}
		cfg.EEAPrefList = strings.TrimSpace(s)
	case "eia_pref_list":
		s, ok := asString(val)
		if !ok {
			return fmt.Errorf("eia_pref_list must be string")
		}
		cfg.EIAPrefList = strings.TrimSpace(s)
	case "gtpu_tunnel_timeout":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("gtpu_tunnel_timeout must be int32")
		}
		cfg.GTPUTunnelTimeout = i
	case "s1_setup_max_retries":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("s1_setup_max_retries must be int32")
		}
		cfg.S1SetupMaxRetries = i
	case "s1_connect_timer":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("s1_connect_timer must be int32")
		}
		cfg.S1ConnectTimer = i
	case "rx_gain_offset":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("rx_gain_offset must be number")
		}
		cfg.RXGainOffset = f
	case "use_cedron_f_est_alg":
		switch x := val.(type) {
		case bool:
			cfg.UseCedronFEstAlg = x
		default:
			return fmt.Errorf("use_cedron_f_est_alg must be boolean")
		}
	case "rlf_min_ul_snr_estim":
		f, ok := asFloat64(val)
		if !ok {
			return fmt.Errorf("rlf_min_ul_snr_estim must be number")
		}
		cfg.RLFMinULSNREstim = f
	case "max_mac_dl_kos":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("max_mac_dl_kos must be int32")
		}
		cfg.MaxMacDLKOs = i
	case "max_mac_ul_kos":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("max_mac_ul_kos must be int32")
		}
		cfg.MaxMacULKOs = i
	}
	return nil
}

func applyQCIProfileChange(cfg *EditableConfig, key string, val any) error {
	// key format: qci_profiles[<qci>].<field>
	re := regexp.MustCompile(`^qci_profiles\[(\d+)\]\.([A-Za-z0-9_]+)$`)
	m := re.FindStringSubmatch(key)
	if len(m) != 3 {
		return fmt.Errorf("invalid qci_profiles key: %s", key)
	}
	qci64, _ := strconv.ParseInt(m[1], 10, 32)
	qci := int32(qci64)
	if qci < 1 || qci > 9 {
		return fmt.Errorf("qci must be in range 1..9")
	}
	field := m[2]

	// Find or create.
	idx := -1
	for i := range cfg.QCIProfiles {
		if cfg.QCIProfiles[i].QCI == qci {
			idx = i
			break
		}
	}
	if idx == -1 {
		cfg.QCIProfiles = append(cfg.QCIProfiles, QCIProfile{QCI: qci})
		idx = len(cfg.QCIProfiles) - 1
	}
	p := cfg.QCIProfiles[idx]

	switch field {
	case "discard_timer":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("discard_timer must be int32")
		}
		p.DiscardTimer = i
	case "pdcp_sn_size":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("pdcp_sn_size must be int32")
		}
		p.PDCPSNSize = i
	case "t_poll_retx":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("t_poll_retx must be int32")
		}
		p.TPollRetx = i
	case "max_retx_thresh":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("max_retx_thresh must be int32")
		}
		p.MaxRetxThresh = i
	case "t_reordering":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("t_reordering must be int32")
		}
		p.TReordering = i
	case "priority":
		i, ok := asInt32(val)
		if !ok {
			return fmt.Errorf("priority must be int32")
		}
		p.Priority = i
	default:
		return fmt.Errorf("unsupported qci profile field: %s", field)
	}
	cfg.QCIProfiles[idx] = p
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
	if strings.TrimSpace(cfg.ENBID) == "" || len(cfg.ENBID) > 64 {
		return fmt.Errorf("enb_id is invalid")
	}
	if strings.TrimSpace(cfg.MMEAddr) == "" || len(cfg.MMEAddr) > 64 {
		return fmt.Errorf("mme_addr is invalid")
	}
	if ip := net.ParseIP(strings.TrimSpace(cfg.MMEAddr)); ip == nil {
		return fmt.Errorf("mme_addr must be a valid IP address")
	}
	if strings.TrimSpace(cfg.GTPBindAddr) == "" || len(cfg.GTPBindAddr) > 64 {
		return fmt.Errorf("gtp_bind_addr is invalid")
	}
	if ip := net.ParseIP(strings.TrimSpace(cfg.GTPBindAddr)); ip == nil {
		return fmt.Errorf("gtp_bind_addr must be a valid IP address")
	}
	if strings.TrimSpace(cfg.S1CBindAddr) == "" || len(cfg.S1CBindAddr) > 64 {
		return fmt.Errorf("s1c_bind_addr is invalid")
	}
	if ip := net.ParseIP(strings.TrimSpace(cfg.S1CBindAddr)); ip == nil {
		return fmt.Errorf("s1c_bind_addr must be a valid IP address")
	}
	if cfg.S1CBindPort > 65535 {
		return fmt.Errorf("s1c_bind_port must be in range 0..65535")
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
	if cfg.RXGain < 0 || cfg.RXGain > 120 {
		return fmt.Errorf("rx_gain must be in range 0..120")
	}
	if len(cfg.DeviceName) > 128 {
		return fmt.Errorf("device_name is too long")
	}
	if len(cfg.DeviceArgs) > 512 {
		return fmt.Errorf("device_args is too long")
	}
	switch cfg.NPRB {
	case 6, 15, 25, 50, 75, 100:
	default:
		return fmt.Errorf("n_prb must be one of 6,15,25,50,75,100")
	}
	if cfg.TM != 0 && (cfg.TM < 1 || cfg.TM > 4) {
		return fmt.Errorf("tm must be in range 1..4")
	}
	if cfg.MinCtrlSymbols != 0 && (cfg.MinCtrlSymbols < 1 || cfg.MinCtrlSymbols > 3) {
		return fmt.Errorf("min_nof_ctrl_symbols must be in range 1..3")
	}
	if cfg.MaxCtrlSymbols != 0 && (cfg.MaxCtrlSymbols < 1 || cfg.MaxCtrlSymbols > 3) {
		return fmt.Errorf("max_nof_ctrl_symbols must be in range 1..3")
	}
	if cfg.TargetBLER != 0 && (cfg.TargetBLER < 0.0 || cfg.TargetBLER > 1.0) {
		return fmt.Errorf("target_bler must be in range 0..1")
	}
	if strings.TrimSpace(cfg.CellID) == "" {
		return fmt.Errorf("cell_id is invalid")
	}
	if strings.TrimSpace(cfg.TAC) == "" {
		return fmt.Errorf("tac is invalid")
	}
	for _, p := range cfg.QCIProfiles {
		if p.QCI < 1 || p.QCI > 9 {
			return fmt.Errorf("qci must be in range 1..9")
		}
	}
	return nil
}

func writeENBConfig(path string, cfg EditableConfig, keys map[string]struct{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(b)
	if _, ok := keys["enb_id"]; ok {
		src = replaceInSection(src, "enb", "enb_id", cfg.ENBID)
	}
	if _, ok := keys["mcc"]; ok {
		src = replaceInSection(src, "enb", "mcc", cfg.MCC)
	}
	if _, ok := keys["mnc"]; ok {
		src = replaceInSection(src, "enb", "mnc", cfg.MNC)
	}
	if _, ok := keys["mme_addr"]; ok {
		src = replaceInSection(src, "enb", "mme_addr", cfg.MMEAddr)
	}
	if _, ok := keys["gtp_bind_addr"]; ok {
		src = replaceInSection(src, "enb", "gtp_bind_addr", cfg.GTPBindAddr)
	}
	if _, ok := keys["s1c_bind_addr"]; ok {
		src = replaceInSection(src, "enb", "s1c_bind_addr", cfg.S1CBindAddr)
	}
	if _, ok := keys["s1c_bind_port"]; ok {
		src = replaceInSection(src, "enb", "s1c_bind_port", strconv.FormatUint(uint64(cfg.S1CBindPort), 10))
	}
	if _, ok := keys["n_prb"]; ok {
		src = replaceInSection(src, "enb", "n_prb", strconv.FormatUint(uint64(cfg.NPRB), 10))
	}
	if _, ok := keys["tm"]; ok {
		src = replaceInSection(src, "enb", "tm", strconv.FormatUint(uint64(cfg.TM), 10))
	}

	if _, ok := keys["tx_gain"]; ok {
		src = replaceInSection(src, "rf", "tx_gain", trimFloat(cfg.TXGain))
	}
	if _, ok := keys["rx_gain"]; ok {
		src = replaceInSection(src, "rf", "rx_gain", trimFloat(cfg.RXGain))
	}
	if _, ok := keys["device_name"]; ok && strings.TrimSpace(cfg.DeviceName) != "" {
		src = replaceInSection(src, "rf", "device_name", cfg.DeviceName)
	}
	if _, ok := keys["device_args"]; ok && strings.TrimSpace(cfg.DeviceArgs) != "" {
		src = replaceInSection(src, "rf", "device_args", cfg.DeviceArgs)
	}
	if _, ok := keys["time_adv_nsamples"]; ok && strings.TrimSpace(cfg.TimeAdvNSamples) != "" {
		src = replaceInSection(src, "rf", "time_adv_nsamples", cfg.TimeAdvNSamples)
	}

	if _, ok := keys["sched_policy"]; ok && strings.TrimSpace(cfg.SchedPolicy) != "" {
		src = replaceInSection(src, "scheduler", "policy", cfg.SchedPolicy)
	}
	if _, ok := keys["pdsch_max_mcs"]; ok {
		src = replaceInSection(src, "scheduler", "pdsch_max_mcs", strconv.FormatInt(int64(cfg.PDSCHMaxMCS), 10))
	}
	if _, ok := keys["pusch_max_mcs"]; ok {
		src = replaceInSection(src, "scheduler", "pusch_max_mcs", strconv.FormatInt(int64(cfg.PUSCHMaxMCS), 10))
	}
	if _, ok := keys["target_bler"]; ok {
		src = replaceInSection(src, "scheduler", "target_bler", trimFloat(cfg.TargetBLER))
	}
	if _, ok := keys["min_nof_ctrl_symbols"]; ok {
		src = replaceInSection(src, "scheduler", "min_nof_ctrl_symbols", strconv.FormatInt(int64(cfg.MinCtrlSymbols), 10))
	}
	if _, ok := keys["max_nof_ctrl_symbols"]; ok {
		src = replaceInSection(src, "scheduler", "max_nof_ctrl_symbols", strconv.FormatInt(int64(cfg.MaxCtrlSymbols), 10))
	}

	if _, ok := keys["pusch_max_its"]; ok {
		src = replaceInSection(src, "expert", "pusch_max_its", strconv.FormatInt(int64(cfg.PUSCHMaxIts), 10))
	}
	if _, ok := keys["nr_pusch_max_its"]; ok {
		src = replaceInSection(src, "expert", "nr_pusch_max_its", strconv.FormatInt(int64(cfg.NRPUSCHMaxIts), 10))
	}
	if _, ok := keys["pusch_8bit_decoder"]; ok {
		src = replaceInSection(src, "expert", "pusch_8bit_decoder", strconv.FormatBool(cfg.PUSCH8bitDecoder))
	}
	if _, ok := keys["nof_phy_threads"]; ok {
		src = replaceInSection(src, "expert", "nof_phy_threads", strconv.FormatInt(int64(cfg.NofPHYThreads), 10))
	}
	if _, ok := keys["metrics_period_secs"]; ok {
		src = replaceInSection(src, "expert", "metrics_period_secs", strconv.FormatInt(int64(cfg.MetricsPeriodSecs), 10))
	}
	if _, ok := keys["tx_amplitude"]; ok {
		src = replaceInSection(src, "expert", "tx_amplitude", trimFloat(cfg.TXAmplitude))
	}
	if _, ok := keys["rrc_inactivity_timer"]; ok {
		src = replaceInSection(src, "expert", "rrc_inactivity_timer", strconv.FormatInt(int64(cfg.RRCInactivityTimer), 10))
	}
	if _, ok := keys["rlf_release_timer_ms"]; ok {
		src = replaceInSection(src, "expert", "rlf_release_timer_ms", strconv.FormatInt(int64(cfg.RLFReleaseTimerMs), 10))
	}
	if _, ok := keys["eea_pref_list"]; ok && strings.TrimSpace(cfg.EEAPrefList) != "" {
		src = replaceInSection(src, "expert", "eea_pref_list", cfg.EEAPrefList)
	}
	if _, ok := keys["eia_pref_list"]; ok && strings.TrimSpace(cfg.EIAPrefList) != "" {
		src = replaceInSection(src, "expert", "eia_pref_list", cfg.EIAPrefList)
	}
	if _, ok := keys["gtpu_tunnel_timeout"]; ok {
		src = replaceInSection(src, "expert", "gtpu_tunnel_timeout", strconv.FormatInt(int64(cfg.GTPUTunnelTimeout), 10))
	}
	if _, ok := keys["s1_setup_max_retries"]; ok {
		src = replaceInSection(src, "expert", "s1_setup_max_retries", strconv.FormatInt(int64(cfg.S1SetupMaxRetries), 10))
	}
	if _, ok := keys["s1_connect_timer"]; ok {
		src = replaceInSection(src, "expert", "s1_connect_timer", strconv.FormatInt(int64(cfg.S1ConnectTimer), 10))
	}
	if _, ok := keys["rx_gain_offset"]; ok {
		src = replaceInSection(src, "expert", "rx_gain_offset", trimFloat(cfg.RXGainOffset))
	}
	if _, ok := keys["use_cedron_f_est_alg"]; ok {
		src = replaceInSection(src, "expert", "use_cedron_f_est_alg", strconv.FormatBool(cfg.UseCedronFEstAlg))
	}
	if _, ok := keys["rlf_min_ul_snr_estim"]; ok {
		src = replaceInSection(src, "expert", "rlf_min_ul_snr_estim", trimFloat(cfg.RLFMinULSNREstim))
	}
	if _, ok := keys["max_mac_dl_kos"]; ok {
		src = replaceInSection(src, "expert", "max_mac_dl_kos", strconv.FormatInt(int64(cfg.MaxMacDLKOs), 10))
	}
	if _, ok := keys["max_mac_ul_kos"]; ok {
		src = replaceInSection(src, "expert", "max_mac_ul_kos", strconv.FormatInt(int64(cfg.MaxMacULKOs), 10))
	}
	if _, ok := keys["enb_serial"]; ok {
		src = replaceInSection(src, "expert", "enb_serial", cfg.ENBSerial)
	}
	return atomicWrite(path, []byte(src))
}

func writeRRConfig(path string, cfg EditableConfig, keys map[string]struct{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(b)
	if _, ok := keys["cell_id"]; ok {
		src = replaceFirstKey(src, "cell_id", cfg.CellID)
	}
	if _, ok := keys["tac"]; ok {
		src = replaceFirstKey(src, "tac", cfg.TAC)
	}
	if _, ok := keys["dl_earfcn"]; ok {
		src = replaceFirstKey(src, "dl_earfcn", strconv.FormatUint(uint64(cfg.DLEARFCN), 10))
	}
	if _, ok := keys["pci"]; ok {
		src = replaceFirstKey(src, "pci", strconv.FormatUint(uint64(cfg.PCI), 10))
	}
	if _, ok := keys["ho_active"]; ok {
		src = replaceFirstKey(src, "ho_active", strconv.FormatBool(cfg.HOActive))
	}
	if _, ok := keys["a3_offset"]; ok {
		src = replaceFirstKey(src, "a3_offset", strconv.FormatInt(int64(cfg.A3Offset), 10))
	}
	if _, ok := keys["time_to_trigger"]; ok {
		src = replaceFirstKey(src, "time_to_trigger", strconv.FormatInt(int64(cfg.TimeToTrigger), 10))
	}
	if _, ok := keys["hysteresis"]; ok {
		src = replaceFirstKey(src, "hysteresis", strconv.FormatInt(int64(cfg.Hysteresis), 10))
	}
	return atomicWrite(path, []byte(src))
}

func writeSIBConfig(path string, cfg EditableConfig, keys map[string]struct{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(b)

	// sib1
	if _, ok := keys["q_rx_lev_min"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib1", "q_rx_lev_min", strconv.FormatInt(int64(cfg.QRxLevMin), 10))
	}
	if _, ok := keys["cell_barred"]; ok && strings.TrimSpace(cfg.CellBarred) != "" {
		src = replaceKVInNamedBraceBlock(src, "sib1", "cell_barred", fmt.Sprintf("%q", cfg.CellBarred))
	}

	// sib2 -> rach_cnfg
	if _, ok := keys["num_ra_preambles"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "num_ra_preambles", strconv.FormatInt(int64(cfg.NumRAPreambles), 10))
	}
	if _, ok := keys["preamble_init_rx_target_pwr"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "preamble_init_rx_target_pwr", strconv.FormatInt(int64(cfg.PreambleInitRxTargetPwr), 10))
	}
	if _, ok := keys["pwr_ramping_step"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "pwr_ramping_step", strconv.FormatInt(int64(cfg.PwrRampingStep), 10))
	}

	// sib2 -> pdsch_cnfg (rs_power)
	if _, ok := keys["reference_signal_power"]; ok && cfg.ReferenceSignalPower != 0 {
		src = replaceKVInNamedBraceBlock(src, "sib2", "rs_power", strconv.FormatInt(int64(cfg.ReferenceSignalPower), 10))
	}

	// sib2 -> pcch_cnfg
	if _, ok := keys["default_paging_cycle"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "default_paging_cycle", strconv.FormatInt(int64(cfg.DefaultPagingCycle), 10))
	}

	// sib2 -> ul_pwr_ctrl
	if _, ok := keys["p0_nominal_pusch"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "p0_nominal_pusch", strconv.FormatInt(int64(cfg.P0NominalPUSCH), 10))
	}
	if _, ok := keys["p0_nominal_pucch"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "p0_nominal_pucch", strconv.FormatInt(int64(cfg.P0NominalPUCCH), 10))
	}
	if _, ok := keys["alpha"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "alpha", trimFloat(cfg.Alpha))
	}

	// timers
	if _, ok := keys["t300"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "t300", strconv.FormatInt(int64(cfg.T300), 10))
	}
	if _, ok := keys["t301"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "t301", strconv.FormatInt(int64(cfg.T301), 10))
	}
	if _, ok := keys["t310"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "t310", strconv.FormatInt(int64(cfg.T310), 10))
	}
	if _, ok := keys["n310"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "n310", strconv.FormatInt(int64(cfg.N310), 10))
	}
	if _, ok := keys["t311"]; ok {
		src = replaceKVInNamedBraceBlock(src, "sib2", "t311", strconv.FormatInt(int64(cfg.T311), 10))
	}

	return atomicWrite(path, []byte(src))
}

func writeRBConfig(path string, cfg EditableConfig, keys map[string]struct{}) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	src := string(b)

	edited := false
	for k := range keys {
		if strings.HasPrefix(k, "qci_profiles[") {
			edited = true
			break
		}
	}
	if edited {
		src = upsertRBQCIProfiles(src, cfg.QCIProfiles)
	}
	return atomicWrite(path, []byte(src))
}

func upsertRBQCIProfiles(src string, profiles []QCIProfile) string {
	if len(profiles) == 0 {
		return src
	}
	anchor := "qci_config"
	idx := strings.Index(src, anchor)
	if idx < 0 {
		return src
	}
	pIdx := strings.Index(src[idx:], "(")
	if pIdx < 0 {
		return src
	}
	pIdx = idx + pIdx
	section, endIdx, ok := extractParenSpan(src, pIdx)
	if !ok {
		return src
	}

	type span struct {
		start, end int
		qci        int32
	}
	spans := []span{}
	// Scan section for top-level objects.
	depth := 0
	objStart := -1
	for i := 0; i < len(section); i++ {
		switch section[i] {
		case '{':
			depth++
			if depth == 1 {
				objStart = i
			}
		case '}':
			if depth == 1 && objStart >= 0 {
				obj := section[objStart : i+1]
				qci := parseQCIFromRBObject(obj)
				spans = append(spans, span{start: objStart, end: i + 1, qci: qci})
				objStart = -1
			}
			if depth > 0 {
				depth--
			}
		}
	}

	updated := section
	for i := len(spans) - 1; i >= 0; i-- {
		sp := spans[i]
		if sp.qci == 0 {
			continue
		}
		var desired *QCIProfile
		for j := range profiles {
			if profiles[j].QCI == sp.qci {
				desired = &profiles[j]
				break
			}
		}
		if desired == nil {
			continue
		}
		obj := updated[sp.start:sp.end]
		obj = replaceFirstKey(obj, "discard_timer", strconv.FormatInt(int64(desired.DiscardTimer), 10))
		if desired.PDCPSNSize != 0 {
			obj = replaceFirstKey(obj, "pdcp_sn_size", strconv.FormatInt(int64(desired.PDCPSNSize), 10))
		}
		if desired.TPollRetx != 0 {
			obj = replaceFirstKey(obj, "t_poll_retx", strconv.FormatInt(int64(desired.TPollRetx), 10))
		}
		if desired.MaxRetxThresh != 0 {
			obj = replaceFirstKey(obj, "max_retx_thresh", strconv.FormatInt(int64(desired.MaxRetxThresh), 10))
		}
		if desired.TReordering != 0 {
			obj = replaceFirstKey(obj, "t_reordering", strconv.FormatInt(int64(desired.TReordering), 10))
		}
		if desired.Priority != 0 {
			obj = replaceFirstKey(obj, "priority", strconv.FormatInt(int64(desired.Priority), 10))
		}
		updated = updated[:sp.start] + obj + updated[sp.end:]
	}

	for _, p := range profiles {
		found := false
		for _, sp := range spans {
			if sp.qci == p.QCI {
				found = true
				break
			}
		}
		if found {
			continue
		}
		ins := fmt.Sprintf("\n{\n  qci = %d;\n  pdcp_config = { discard_timer = %d; pdcp_sn_size = %d; };\n  logical_channel_config = { priority = %d; };\n},\n",
			p.QCI, p.DiscardTimer, p.PDCPSNSize, p.Priority)
		if k := strings.LastIndex(updated, ")"); k > 0 {
			updated = updated[:k] + ins + updated[k:]
		}
	}

	return src[:pIdx] + updated + src[endIdx:]
}

func parseQCIFromRBObject(obj string) int32 {
	re := regexp.MustCompile(`(?m)^\s*qci\s*=\s*([0-9]+)\s*;`)
	m := re.FindStringSubmatch(obj)
	if len(m) != 2 {
		return 0
	}
	v, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil {
		return 0
	}
	return int32(v)
}

func extractParenSpan(src string, parenIdx int) (section string, endIdx int, ok bool) {
	if parenIdx < 0 || parenIdx >= len(src) || src[parenIdx] != '(' {
		return "", 0, false
	}
	depth := 0
	for i := parenIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[parenIdx : i+1], i + 1, true
			}
		}
	}
	return "", 0, false
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

func replaceKVInNamedBraceBlock(src, blockName, key, value string) string {
	idx := strings.Index(src, blockName)
	if idx < 0 {
		return src
	}
	br := strings.Index(src[idx:], "{")
	if br < 0 {
		return src
	}
	br = idx + br
	block, end, ok := extractBraceBlockSpan(src, br)
	if !ok {
		return src
	}
	block2 := replaceFirstKey(block, key, value)
	return src[:br] + block2 + src[end:]
}

func replaceKVInFirstBraceAfter(src, anchor, key, value string) string {
	idx := strings.Index(src, anchor)
	if idx < 0 {
		return src
	}
	br := strings.Index(src[idx:], "{")
	if br < 0 {
		return src
	}
	br = idx + br
	block, end, ok := extractBraceBlockSpan(src, br)
	if !ok {
		return src
	}
	block2 := replaceFirstKey(block, key, value)
	return src[:br] + block2 + src[end:]
}

func extractBraceBlockSpan(src string, braceIdx int) (block string, endIdx int, ok bool) {
	if braceIdx < 0 || braceIdx >= len(src) || src[braceIdx] != '{' {
		return "", 0, false
	}
	depth := 0
	for i := braceIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[braceIdx : i+1], i + 1, true
			}
		}
	}
	return "", 0, false
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

func asInt32(v any) (int32, bool) {
	const minInt32 = -2147483648
	const maxInt32 = 2147483647
	switch x := v.(type) {
	case float64:
		if x < minInt32 || x > maxInt32 || math.Trunc(x) != x {
			return 0, false
		}
		return int32(x), true
	case int:
		if x < minInt32 || x > maxInt32 {
			return 0, false
		}
		return int32(x), true
	case int32:
		return x, true
	case int64:
		if x < minInt32 || x > maxInt32 {
			return 0, false
		}
		return int32(x), true
	case json.Number:
		i, err := strconv.ParseInt(string(x), 10, 32)
		return int32(i), err == nil
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
