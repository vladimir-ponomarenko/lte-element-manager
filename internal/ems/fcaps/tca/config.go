package tca

import "time"

// Config groups all threshold crossing alert rules.
type Config struct {
	Enabled bool
	Rules   map[string]RuleConfig
}

// RuleConfig defines raise/clear thresholds and time-to-trigger windows.
type RuleConfig struct {
	Enabled        bool
	RaiseThreshold float64
	ClearThreshold float64
	RaiseDuration  time.Duration
	ClearDuration  time.Duration
}

const (
	RuleS1InterfaceDown        = "s1_interface_down"
	RuleNASSignalingLoss       = "nas_signaling_loss"
	RuleNASSecurityMismatch    = "nas_security_mismatch"
	RuleNASParsingFailure      = "nas_parsing_failure"
	RuleRRCProtocolError       = "rrc_protocol_error"
	RuleRRCConnectionRejection = "rrc_connection_rejection"
	RuleCoreServiceReject      = "core_service_reject"
	RulePagingCapacityExceeded = "paging_capacity_exceeded"
	RuleRLCMaxRetransmissions  = "rlc_max_retransmissions"
	RuleLowThroughput          = "low_throughput"
	RuleBadSignalCondition     = "bad_signal_condition"
	RuleHighBLER               = "high_bler"
	RuleRLFStorm               = "radio_link_failure_storm"
	RuleRFInterference         = "rf_interference_detected"
	RuleUEInactivityCleanup    = "ue_inactivity_cleanup"
	RuleBearerCongestion       = "bearer_congestion"
	RulePowerHeadroomCritical  = "power_headroom_critical"
)

// DemoSafeConfig returns short but stable thresholds suitable for lab demos.
func DemoSafeConfig() Config {
	return Config{
		Enabled: true,
		Rules: map[string]RuleConfig{
			RuleS1InterfaceDown:        immediateRule(1, 1, 0, 10*time.Second),
			RuleNASSignalingLoss:       immediateRule(0, 0, 0, 30*time.Second),
			RuleNASSecurityMismatch:    immediateRule(0, 0, 0, 30*time.Second),
			RuleNASParsingFailure:      immediateRule(0, 0, 0, 30*time.Second),
			RuleRRCProtocolError:       immediateRule(0, 0, 0, 30*time.Second),
			RuleRRCConnectionRejection: immediateRule(0, 0, 0, 30*time.Second),
			RuleCoreServiceReject:      immediateRule(0, 0, 0, 30*time.Second),
			RulePagingCapacityExceeded: immediateRule(0, 0, 0, 30*time.Second),
			RuleRLCMaxRetransmissions:  immediateRule(0, 0, 0, 30*time.Second),
			RuleLowThroughput:          immediateRule(1_000_000, 1_000_001, 30*time.Second, 10*time.Second),
			RuleBadSignalCondition:     immediateRule(0, 5, 10*time.Second, 10*time.Second),
			RuleHighBLER:               immediateRule(0.10, 0.05, 10*time.Second, 10*time.Second),
			RuleRLFStorm:               immediateRule(5, 0, 0, 30*time.Second),
			RuleRFInterference:         immediateRule(-90, -95, 10*time.Second, 10*time.Second),
			RuleUEInactivityCleanup:    immediateRule(0.8, 0.1, 30*time.Second, 30*time.Second),
			RuleBearerCongestion:       immediateRule(500_000, 100_000, 10*time.Second, 10*time.Second),
			RulePowerHeadroomCritical:  immediateRule(0, 3, 10*time.Second, 10*time.Second),
		},
	}
}

// NormalizeConfig overlays configured rules on top of demo-safe defaults.
func NormalizeConfig(in Config) Config {
	if !in.Enabled {
		return Config{}
	}
	def := DemoSafeConfig()
	if def.Rules == nil {
		def.Rules = map[string]RuleConfig{}
	}
	for name, rule := range in.Rules {
		base := def.Rules[name]
		def.Rules[name] = normalizeRule(base, rule)
	}
	return def
}

// Rule returns a normalized rule by name.
func (c Config) Rule(name string) RuleConfig {
	if !c.Enabled || c.Rules == nil {
		return RuleConfig{}
	}
	return c.Rules[name]
}

func immediateRule(raise, clear float64, raiseDuration, clearDuration time.Duration) RuleConfig {
	return RuleConfig{
		Enabled:        true,
		RaiseThreshold: raise,
		ClearThreshold: clear,
		RaiseDuration:  raiseDuration,
		ClearDuration:  clearDuration,
	}
}

func normalizeRule(def RuleConfig, in RuleConfig) RuleConfig {
	if !in.Enabled {
		def.Enabled = false
		return def
	}
	def.Enabled = true
	def.RaiseThreshold = in.RaiseThreshold
	def.ClearThreshold = in.ClearThreshold
	if in.RaiseDuration > 0 {
		def.RaiseDuration = in.RaiseDuration
	}
	if in.ClearDuration > 0 {
		def.ClearDuration = in.ClearDuration
	}
	return def
}
