package tca

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/domain/nrm"
	domainpm "lte-element-manager/internal/ems/domain/pm"
	"lte-element-manager/internal/ems/fcaps/alarms"
	pmfcaps "lte-element-manager/internal/ems/fcaps/pm"
)

const componentPrefix = "tca:"

// Engine evaluates PM reports and converts threshold crossings into alarms.
type Engine struct {
	Manager *alarms.Manager
	Log     zerolog.Logger

	cfg    Config
	states map[stateKey]ruleState
}

type stateKey struct {
	rule      string
	component string
	code      string
}

type ruleState struct {
	active       bool
	pendingRaise time.Time
	pendingClear time.Time
}

type thresholdRule struct {
	name     string
	code     string
	config   RuleConfig
	severity string
}

// NewEngine creates a TCA engine with normalized rule defaults.
func NewEngine(manager *alarms.Manager, cfg Config, log zerolog.Logger) *Engine {
	cfg = NormalizeConfig(cfg)
	return &Engine{
		Manager: manager,
		Log:     log,
		cfg:     cfg,
		states:  make(map[stateKey]ruleState),
	}
}

// Enabled reports whether the engine has at least top-level TCA enabled.
func (e *Engine) Enabled() bool {
	return e != nil && e.cfg.Enabled
}

// Evaluate processes a PM report and returns alarm events raised or cleared.
func (e *Engine) Evaluate(report pmfcaps.Report) []alarms.Event {
	if e == nil || e.Manager == nil || !e.cfg.Enabled {
		return nil
	}
	now := report.End
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var out []alarms.Event
	out = append(out, e.evaluateS1Interface(report, now)...)
	out = append(out, e.evaluateNodeCounters(report, now)...)
	out = append(out, e.evaluateLowThroughput(report, now)...)
	out = append(out, e.evaluatePerDN(report, now, RuleBadSignalCondition, alarms.AlarmBadSignalCondition, badSignalValue, belowRaise, aboveClear)...)
	out = append(out, e.evaluatePerDN(report, now, RuleHighBLER, alarms.AlarmHighBLER, maxBLER, aboveRaise, belowClear)...)
	out = append(out, e.evaluatePerDN(report, now, RuleRLFStorm, alarms.AlarmRadioLinkFailureStorm, metricValue(domainpm.CanonicalUERRCRLFCnt), aboveRaise, atOrBelowClear)...)
	out = append(out, e.evaluatePerDN(report, now, RuleRFInterference, alarms.AlarmRFInterferenceDetected, metricValue(domainpm.CanonicalUEULPUCCHNI), aboveRaise, belowClear)...)
	out = append(out, e.evaluatePerDN(report, now, RuleUEInactivityCleanup, alarms.AlarmUEInactivityCleanup, metricValue(domainpm.CanonicalUERRCInactivity), aboveRaise, belowClear)...)
	out = append(out, e.evaluateBearerCongestion(report, now)...)
	out = append(out, e.evaluatePerDN(report, now, RulePowerHeadroomCritical, alarms.AlarmPowerHeadroomCritical, metricValue(domainpm.CanonicalUEULPHR), atOrBelowRaise, aboveClear)...)
	return out
}

func (e *Engine) evaluateS1Interface(report pmfcaps.Report, now time.Time) []alarms.Event {
	cfg := e.cfg.Rule(RuleS1InterfaceDown)
	if !cfg.Enabled {
		return nil
	}
	return e.evaluatePerDNWithRule(report, now, thresholdRule{name: RuleS1InterfaceDown, code: alarms.AlarmS1InterfaceDown, config: cfg},
		metricValue(domainpm.CanonicalS1APReady),
		func(v float64, cfg RuleConfig) bool { return v < cfg.RaiseThreshold },
		func(v float64, cfg RuleConfig) bool { return v >= cfg.ClearThreshold },
	)
}

func (e *Engine) evaluateNodeCounters(report pmfcaps.Report, now time.Time) []alarms.Event {
	var out []alarms.Event
	rules := []struct {
		rule      string
		code      string
		keys      []string
		majorAt   float64
		escalates bool
	}{
		{rule: RuleNASSignalingLoss, code: alarms.AlarmNASSignalingLoss, keys: []string{domainpm.CanonicalNASDLDrop, domainpm.CanonicalNASULFail}},
		{rule: RuleNASSecurityMismatch, code: alarms.AlarmNASSecurityMismatch, keys: []string{domainpm.CanonicalNASULSecUnknown}},
		{rule: RuleNASParsingFailure, code: alarms.AlarmNASParsingFailure, keys: []string{domainpm.CanonicalNASULParseFail, domainpm.CanonicalNASDLParseFail}},
		{rule: RuleRRCProtocolError, code: alarms.AlarmRRCProtocolError, keys: []string{domainpm.CanonicalRRCProtocolFail}},
		{rule: RuleRRCConnectionRejection, code: alarms.AlarmRRCConnectionRejection, keys: []string{domainpm.CanonicalRRCConRejectTX}, majorAt: 5, escalates: true},
		{rule: RuleCoreServiceReject, code: alarms.AlarmCoreServiceReject, keys: []string{domainpm.CanonicalNASDLServiceRej}},
		{rule: RulePagingCapacityExceeded, code: alarms.AlarmPagingCapacityExceeded, keys: []string{domainpm.CanonicalRRCPagingFail}},
		{rule: RuleRLCMaxRetransmissions, code: alarms.AlarmRLCMaxRetransmissions, keys: []string{domainpm.CanonicalRRCMaxRLCRetx}},
	}
	for _, item := range rules {
		cfg := e.cfg.Rule(item.rule)
		if !cfg.Enabled {
			continue
		}
		for dn, values := range report.ByDN {
			value, ok := sumValues(values, item.keys...)
			if !ok {
				continue
			}
			severity := ""
			if item.escalates && value >= item.majorAt {
				severity = alarms.SeverityMajor
			}
			out = append(out, e.transition(now,
				thresholdRule{name: item.rule, code: item.code, config: cfg, severity: severity},
				dn,
				value,
				value > cfg.RaiseThreshold,
				value <= cfg.ClearThreshold,
				fmt.Sprintf("%s delta %.0f", item.rule, value),
			)...)
		}
	}
	return out
}

func (e *Engine) evaluateLowThroughput(report pmfcaps.Report, now time.Time) []alarms.Event {
	cfg := e.cfg.Rule(RuleLowThroughput)
	if !cfg.Enabled {
		return nil
	}
	var totalThroughput float64
	var connectedUEs float64
	var moi nrm.DN
	for dn, values := range report.ByDN {
		if moi == "" {
			moi = dn
		}
		if dl, ok := values[domainpm.CanonicalUEDLBitrate]; ok {
			totalThroughput += dl.Value
		}
		if ul, ok := values[domainpm.CanonicalUEULBitrate]; ok {
			totalThroughput += ul.Value
		}
		if connected, ok := values[domainpm.CanonicalRRCConnectedUES]; ok && connected.Value > connectedUEs {
			connectedUEs = connected.Value
			moi = dn
		}
	}
	if moi == "" {
		moi = "SubNetwork=unknown/ManagedElement=unknown"
	}
	violation := connectedUEs > 0 && totalThroughput < cfg.RaiseThreshold
	clear := connectedUEs <= 0 || totalThroughput >= cfg.ClearThreshold
	return e.transition(
		now,
		thresholdRule{name: RuleLowThroughput, code: alarms.AlarmLowThroughputActiveUsers, config: cfg},
		moi,
		totalThroughput,
		violation,
		clear,
		fmt.Sprintf("aggregate throughput %.3f bps with connected UEs %.0f", totalThroughput, connectedUEs),
	)
}

func (e *Engine) evaluateBearerCongestion(report pmfcaps.Report, now time.Time) []alarms.Event {
	cfg := e.cfg.Rule(RuleBearerCongestion)
	if !cfg.Enabled {
		return nil
	}
	var out []alarms.Event
	for dn, values := range report.ByDN {
		for key, value := range values {
			if !strings.HasPrefix(key, "bearer.") || !strings.HasSuffix(key, ".dl_buffered_bytes") {
				continue
			}
			v := value.Value
			out = append(out, e.transition(
				now,
				thresholdRule{name: RuleBearerCongestion, code: alarms.AlarmBearerCongestion, config: cfg},
				dn,
				v,
				v > cfg.RaiseThreshold,
				v <= cfg.ClearThreshold,
				fmt.Sprintf("%s buffered %.0f bytes", key, v),
			)...)
		}
	}
	return out
}

func (e *Engine) evaluatePerDN(
	report pmfcaps.Report,
	now time.Time,
	ruleName string,
	code string,
	valueFn func(map[string]pmfcaps.Value) (float64, bool),
	violationFn func(float64, RuleConfig) bool,
	clearFn func(float64, RuleConfig) bool,
) []alarms.Event {
	cfg := e.cfg.Rule(ruleName)
	if !cfg.Enabled {
		return nil
	}
	return e.evaluatePerDNWithRule(report, now, thresholdRule{name: ruleName, code: code, config: cfg}, valueFn, violationFn, clearFn)
}

func (e *Engine) evaluatePerDNWithRule(
	report pmfcaps.Report,
	now time.Time,
	rule thresholdRule,
	valueFn func(map[string]pmfcaps.Value) (float64, bool),
	violationFn func(float64, RuleConfig) bool,
	clearFn func(float64, RuleConfig) bool,
) []alarms.Event {
	var out []alarms.Event
	for dn, values := range report.ByDN {
		value, ok := valueFn(values)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		out = append(out, e.transition(
			now,
			rule,
			dn,
			value,
			violationFn(value, rule.config),
			clearFn(value, rule.config),
			fmt.Sprintf("%s value %.6f", rule.name, value),
		)...)
	}
	return out
}

func (e *Engine) transition(now time.Time, rule thresholdRule, moi nrm.DN, value float64, violation bool, clear bool, detail string) []alarms.Event {
	component := componentPrefix + string(moi)
	key := stateKey{rule: rule.name, component: component, code: rule.code}
	state := e.states[key]

	if !state.active {
		if !violation {
			if !state.pendingRaise.IsZero() {
				e.Log.Debug().Str("rule", rule.name).Str("moi", string(moi)).Msg("tca pending raise cancelled")
			}
			delete(e.states, key)
			return nil
		}
		if state.pendingRaise.IsZero() {
			state.pendingRaise = now
			e.states[key] = state
			e.Log.Debug().Str("rule", rule.name).Str("moi", string(moi)).Float64("value", value).Dur("ttt", rule.config.RaiseDuration).Msg("tca pending raise started")
			if rule.config.RaiseDuration > 0 {
				return nil
			}
		}
		if now.Sub(state.pendingRaise) < rule.config.RaiseDuration {
			e.states[key] = state
			return nil
		}
		state.active = true
		state.pendingRaise = time.Time{}
		e.states[key] = state
		alarm := alarms.NewThresholdAlarmWithSeverity(rule.code, string(moi), detail, value, rule.severity)
		evt, changed := e.Manager.Raise(now, component, "degraded", alarm)
		e.Log.Warn().Str("rule", rule.name).Str("moi", string(moi)).Str("alarm", rule.code).Float64("value", value).Msg("tca alarm raised")
		if changed {
			return []alarms.Event{evt}
		}
		return nil
	}

	if clear {
		if state.pendingClear.IsZero() {
			state.pendingClear = now
			e.states[key] = state
			e.Log.Debug().Str("rule", rule.name).Str("moi", string(moi)).Float64("value", value).Dur("ttt", rule.config.ClearDuration).Msg("tca pending clear started")
			if rule.config.ClearDuration > 0 {
				return nil
			}
		}
		if now.Sub(state.pendingClear) < rule.config.ClearDuration {
			e.states[key] = state
			return nil
		}
		delete(e.states, key)
		evt, changed := e.Manager.Clear(now, component, rule.code, "healthy")
		e.Log.Info().Str("rule", rule.name).Str("moi", string(moi)).Str("alarm", rule.code).Float64("value", value).Msg("tca alarm cleared")
		if changed {
			return []alarms.Event{evt}
		}
		return nil
	}

	if violation {
		state.pendingClear = time.Time{}
		e.states[key] = state
		e.Manager.Touch(now, component, rule.code)
		return nil
	}

	state.pendingClear = time.Time{}
	e.states[key] = state
	return nil
}

func metricValue(key string) func(map[string]pmfcaps.Value) (float64, bool) {
	return func(values map[string]pmfcaps.Value) (float64, bool) {
		v, ok := values[key]
		return v.Value, ok
	}
}

func maxBLER(values map[string]pmfcaps.Value) (float64, bool) {
	var max float64
	ok := false
	for _, key := range []string{domainpm.CanonicalUEDLBLER, domainpm.CanonicalUEULBLER} {
		v, found := values[key]
		if !found {
			continue
		}
		if !ok || v.Value > max {
			max = v.Value
			ok = true
		}
	}
	return max, ok
}

func badSignalValue(values map[string]pmfcaps.Value) (float64, bool) {
	snr, hasSNR := values[domainpm.CanonicalUEULSNR]
	cqi, hasCQI := values[domainpm.CanonicalUEDLCQI]
	switch {
	case hasSNR && snr.Value < 0:
		return snr.Value, true
	case hasCQI:
		return cqi.Value, true
	case hasSNR:
		return snr.Value, true
	default:
		return 0, false
	}
}

func sumValues(values map[string]pmfcaps.Value, keys ...string) (float64, bool) {
	var out float64
	var ok bool
	for _, key := range keys {
		if v, found := values[key]; found {
			out += v.Value
			ok = true
		}
	}
	return out, ok
}

func belowRaise(v float64, cfg RuleConfig) bool {
	return v < cfg.RaiseThreshold
}

func aboveRaise(v float64, cfg RuleConfig) bool {
	return v > cfg.RaiseThreshold
}

func atOrBelowRaise(v float64, cfg RuleConfig) bool {
	return v <= cfg.RaiseThreshold
}

func belowClear(v float64, cfg RuleConfig) bool {
	return v < cfg.ClearThreshold
}

func atOrBelowClear(v float64, cfg RuleConfig) bool {
	return v <= cfg.ClearThreshold
}

func aboveClear(v float64, cfg RuleConfig) bool {
	return v > cfg.ClearThreshold
}

// ComponentMOI strips the internal TCA component prefix and returns the NRM DN.
func ComponentMOI(component string) string {
	return strings.TrimPrefix(component, componentPrefix)
}
