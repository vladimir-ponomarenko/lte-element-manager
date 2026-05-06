package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"lte-element-manager/internal/ems/configuration/srsranconf"
)

type EditableConfig struct {
	// Element identity.
	ENBSerial           string `json:"enb_serial"`
	SIBConfigFile       string `json:"-"`
	RRConfigFile        string `json:"-"`
	RBConfigFile        string `json:"-"`
	ReportJSONUDSEnable bool   `json:"-"`
	ReportJSONUDSPath   string `json:"-"`
	AlarmsLogEnable     bool   `json:"-"`
	AlarmsFilename      string `json:"-"`

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
	ENB   []byte
	RR    []byte
	SIB   []byte
	RB    []byte
	Dirty bool
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
		SIBConfigFile: firstNonEmpty(
			strings.TrimSpace(enb.SIBConfigFile),
			filepath.Base(sibPath),
		),
		RRConfigFile: firstNonEmpty(
			strings.TrimSpace(enb.RRConfigFile),
			filepath.Base(rrPath),
		),
		RBConfigFile: firstNonEmpty(
			strings.TrimSpace(enb.RBConfigFile),
			filepath.Base(rbPath),
		),
		ReportJSONUDSEnable: enb.ReportJSONUDS,
		ReportJSONUDSPath:   strings.TrimSpace(enb.ReportJSONUDSPath),
		AlarmsLogEnable:     enb.AlarmsLogEnable,
		AlarmsFilename:      strings.TrimSpace(enb.AlarmsFilename),

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

func (s *Store) HasPendingChanges() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty.enb || s.dirty.rr || s.dirty.sib || s.dirty.rb
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
	staged, err := s.stageDirtyFilesLocked()
	if err != nil {
		_ = s.restoreDirtyFilesLocked(backup)
		return nil, EditableConfig{}, err
	}
	if err := commitStagedFiles(staged); err != nil {
		_ = cleanupStagedFiles(staged)
		_ = s.restoreDirtyFilesLocked(backup)
		return nil, EditableConfig{}, err
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
		backup.Dirty = true
		backup.ENB, err = os.ReadFile(s.enbPath)
		if err != nil {
			return nil, err
		}
	}
	if s.dirty.rr {
		backup.Dirty = true
		backup.RR, err = os.ReadFile(s.rrPath)
		if err != nil {
			return nil, err
		}
	}
	if s.dirty.sib {
		backup.Dirty = true
		backup.SIB, err = os.ReadFile(s.sibPath)
		if err != nil {
			return nil, err
		}
	}
	if s.dirty.rb {
		backup.Dirty = true
		backup.RB, err = os.ReadFile(s.rbPath)
		if err != nil {
			return nil, err
		}
	}
	return backup, nil
}

type stagedFile struct {
	final string
	tmp   string
}

func (s *Store) stageDirtyFilesLocked() ([]stagedFile, error) {
	var staged []stagedFile
	stage := func(path string, data []byte) error {
		tmp, err := writeTempSibling(path, data)
		if err != nil {
			return err
		}
		staged = append(staged, stagedFile{final: path, tmp: tmp})
		return nil
	}
	if s.dirty.enb {
		data, err := renderENBConfig(s.enbPath, s.cand, s.dirtyKeys)
		if err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
		if err := stage(s.enbPath, data); err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
	}
	if s.dirty.rr {
		data, err := renderRRConfig(s.rrPath, s.cand, s.dirtyKeys)
		if err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
		if err := stage(s.rrPath, data); err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
	}
	if s.dirty.sib {
		data, err := renderSIBConfig(s.sibPath, s.cand, s.dirtyKeys)
		if err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
		if err := stage(s.sibPath, data); err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
	}
	if s.dirty.rb {
		data, err := renderRBConfig(s.rbPath, s.cand, s.dirtyKeys)
		if err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
		if err := stage(s.rbPath, data); err != nil {
			_ = cleanupStagedFiles(staged)
			return nil, err
		}
	}
	return staged, nil
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
	qci, field, ok := parseQCIProfileKey(key)
	if !ok {
		return fmt.Errorf("invalid qci_profiles key: %s", key)
	}
	if qci < 1 || qci > 9 {
		return fmt.Errorf("qci must be in range 1..9")
	}

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
	if !isFixedDigits(cfg.MCC, 3) {
		return fmt.Errorf("mcc must match [0-9]{3}")
	}
	if !(isFixedDigits(cfg.MNC, 2) || isFixedDigits(cfg.MNC, 3)) {
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

func isFixedDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func atomicWrite(path string, data []byte) error {
	tmpName, err := writeTempSibling(path, data)
	if err != nil {
		return err
	}
	if err := renameAtomic(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func writeTempSibling(path string, data []byte) (string, error) {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	uid := -1
	gid := -1
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			uid = int(sys.Uid)
			gid = int(sys.Gid)
		}
	}
	tmp, err := os.CreateTemp(dir, ".cfg-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if uid >= 0 && gid >= 0 {
		if err := os.Chown(tmpName, uid, gid); err != nil && !errors.Is(err, syscall.EPERM) {
			tmp.Close()
			_ = os.Remove(tmpName)
			return "", err
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

func commitStagedFiles(staged []stagedFile) error {
	if len(staged) == 0 {
		return nil
	}
	applied := make([]stagedFile, 0, len(staged))
	for _, f := range staged {
		if err := renameAtomic(f.tmp, f.final); err != nil {
			for i := len(applied) - 1; i >= 0; i-- {
				_ = os.Remove(applied[i].tmp)
			}
			return err
		}
		applied = append(applied, f)
	}
	return nil
}

func cleanupStagedFiles(staged []stagedFile) error {
	var first error
	for _, f := range staged {
		if err := os.Remove(f.tmp); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

func renameAtomic(tmp, final string) error {
	if err := os.Rename(tmp, final); err != nil {
		if errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("atomic rename %s -> %s failed: %w (mount the parent directory instead of bind-mounting the file)", tmp, final, err)
		}
		return err
	}
	dir, err := os.Open(filepath.Dir(final))
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
