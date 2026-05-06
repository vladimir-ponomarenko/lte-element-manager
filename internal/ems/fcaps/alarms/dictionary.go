package alarms

import (
	"strings"

	"lte-element-manager/internal/ems/domain"
)

const (
	AlarmS1Down                   = "Alarm_S1_Down"
	AlarmS1InterfaceDown          = "Alarm_S1_Interface_Down"
	AlarmNASSignalingLoss         = "Alarm_NAS_Signaling_Loss"
	AlarmNASSecurityMismatch      = "Alarm_NAS_Security_Mismatch"
	AlarmNASParsingFailure        = "Alarm_NAS_Parsing_Failure"
	AlarmRRCProtocolError         = "Alarm_RRC_Protocol_Error"
	AlarmRRCConnectionRejection   = "Alarm_RRC_Connection_Rejection"
	AlarmCoreServiceReject        = "Alarm_Core_Service_Reject"
	AlarmPagingCapacityExceeded   = "Alarm_Paging_Capacity_Exceeded"
	AlarmRLCMaxRetransmissions    = "Alarm_RLC_Max_Retransmissions"
	AlarmLowThroughput            = "Alarm_Low_Throughput"
	AlarmLowThroughputActiveUsers = "Alarm_Low_Throughput_Active_Users"
	AlarmLowSINR                  = "Alarm_Low_SINR"
	AlarmBadSignalCondition       = "Alarm_Bad_Signal_Condition"
	AlarmHighBLER                 = "Alarm_High_BLER"
	AlarmRadioLinkFailureStorm    = "Alarm_Radio_Link_Failure_Storm"
	AlarmRFInterferenceDetected   = "Alarm_RF_Interference_Detected"
	AlarmUEInactivityCleanup      = "Alarm_UE_Inactivity_Cleanup"
	AlarmBearerCongestion         = "Alarm_Bearer_Congestion"
	AlarmPowerHeadroomCritical    = "Alarm_Power_Headroom_Critical"
	AlarmUDSDisconnected          = "Alarm_UDS_Disconnected"
	AlarmSrsENBProcessDown        = "Alarm_SrsENB_ProcessDown"
	AlarmNetconfDown              = "Alarm_Netconf_Down"
	AlarmGenericEMS               = "Alarm_EMS_Generic"
)

type Definition struct {
	AlarmID           string
	EventType         string
	ProbableCause     string
	PerceivedSeverity string
	SpecificProblem   string
}

var Dictionary = map[string]Definition{
	AlarmS1Down: {
		AlarmID:           AlarmS1Down,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseCommunicationsFail,
		PerceivedSeverity: SeverityCritical,
		SpecificProblem:   "S1AP connectivity is down",
	},
	AlarmS1InterfaceDown: {
		AlarmID:           AlarmS1InterfaceDown,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseCommunicationsFail,
		PerceivedSeverity: SeverityCritical,
		SpecificProblem:   "S1 interface to MME is down",
	},
	AlarmNASSignalingLoss: {
		AlarmID:           AlarmNASSignalingLoss,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseCommunicationsFail,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "NAS signalling messages are dropped or failed",
	},
	AlarmNASSecurityMismatch: {
		AlarmID:           AlarmNASSecurityMismatch,
		EventType:         EventTypeProcessingErrorAlarm,
		ProbableCause:     ProbableCauseSoftwareError,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "Unknown NAS security header was observed",
	},
	AlarmNASParsingFailure: {
		AlarmID:           AlarmNASParsingFailure,
		EventType:         EventTypeProcessingErrorAlarm,
		ProbableCause:     ProbableCauseSoftwareError,
		PerceivedSeverity: SeverityCritical,
		SpecificProblem:   "NAS message parsing failed",
	},
	AlarmRRCProtocolError: {
		AlarmID:           AlarmRRCProtocolError,
		EventType:         EventTypeProcessingErrorAlarm,
		ProbableCause:     ProbableCauseSoftwareError,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "RRC protocol failure was reported",
	},
	AlarmRRCConnectionRejection: {
		AlarmID:           AlarmRRCConnectionRejection,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMinor,
		SpecificProblem:   "RRC connection requests are being rejected",
	},
	AlarmCoreServiceReject: {
		AlarmID:           AlarmCoreServiceReject,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseCommunicationsFail,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "Core network rejected UE service request",
	},
	AlarmPagingCapacityExceeded: {
		AlarmID:           AlarmPagingCapacityExceeded,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "Paging queue capacity was exceeded",
	},
	AlarmRLCMaxRetransmissions: {
		AlarmID:           AlarmRLCMaxRetransmissions,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "RLC maximum retransmission threshold reached",
	},
	AlarmUDSDisconnected: {
		AlarmID:           AlarmUDSDisconnected,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseLANError,
		PerceivedSeverity: SeverityCritical,
		SpecificProblem:   "srsRAN metrics UDS is disconnected",
	},
	AlarmSrsENBProcessDown: {
		AlarmID:           AlarmSrsENBProcessDown,
		EventType:         EventTypeProcessingErrorAlarm,
		ProbableCause:     ProbableCauseSoftwareError,
		PerceivedSeverity: SeverityCritical,
		SpecificProblem:   "srsENB process is not running",
	},
	AlarmLowSINR: {
		AlarmID:           AlarmLowSINR,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "UL SINR threshold crossed",
	},
	AlarmBadSignalCondition: {
		AlarmID:           AlarmBadSignalCondition,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityWarning,
		SpecificProblem:   "Radio signal quality is below acceptable threshold",
	},
	AlarmHighBLER: {
		AlarmID:           AlarmHighBLER,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "BLER threshold crossed",
	},
	AlarmLowThroughput: {
		AlarmID:           AlarmLowThroughput,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "Throughput threshold crossed",
	},
	AlarmLowThroughputActiveUsers: {
		AlarmID:           AlarmLowThroughputActiveUsers,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "Low throughput while RRC users are connected",
	},
	AlarmRadioLinkFailureStorm: {
		AlarmID:           AlarmRadioLinkFailureStorm,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityCritical,
		SpecificProblem:   "Radio link failures are increasing rapidly",
	},
	AlarmRFInterferenceDetected: {
		AlarmID:           AlarmRFInterferenceDetected,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "RF interference is detected on uplink control channel",
	},
	AlarmUEInactivityCleanup: {
		AlarmID:           AlarmUEInactivityCleanup,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityWarning,
		SpecificProblem:   "UE inactivity releases are growing abnormally",
	},
	AlarmBearerCongestion: {
		AlarmID:           AlarmBearerCongestion,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityMinor,
		SpecificProblem:   "Bearer queue is congested",
	},
	AlarmPowerHeadroomCritical: {
		AlarmID:           AlarmPowerHeadroomCritical,
		EventType:         EventTypeQualityOfServiceAlarm,
		ProbableCause:     ProbableCauseThresholdCrossed,
		PerceivedSeverity: SeverityWarning,
		SpecificProblem:   "UE power headroom is critical",
	},
	AlarmNetconfDown: {
		AlarmID:           AlarmNetconfDown,
		EventType:         EventTypeCommunicationsAlarm,
		ProbableCause:     ProbableCauseLANError,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "NETCONF server is down",
	},
	AlarmGenericEMS: {
		AlarmID:           AlarmGenericEMS,
		EventType:         EventTypeProcessingErrorAlarm,
		ProbableCause:     ProbableCauseSoftwareError,
		PerceivedSeverity: SeverityMajor,
		SpecificProblem:   "EMS internal fault",
	},
}

func Normalize(component, managedObject string, alarm domain.Alarm) domain.Alarm {
	code := strings.TrimSpace(alarm.Code)
	if code == "" {
		code = AlarmGenericEMS
	}
	code = canonicalCode(component, code)
	def, ok := Dictionary[code]
	if !ok {
		def = Dictionary[AlarmGenericEMS]
		def.AlarmID = code
	}

	out := alarm
	out.Code = code
	out.AlarmID = firstNonEmpty(out.AlarmID, def.AlarmID, code)
	out.ManagedObjectInstance = firstNonEmpty(out.ManagedObjectInstance, managedObject, component)
	out.EventType = firstNonEmpty(out.EventType, def.EventType)
	out.ProbableCause = firstNonEmpty(out.ProbableCause, def.ProbableCause)
	out.PerceivedSeverity = firstNonEmpty(out.PerceivedSeverity, def.PerceivedSeverity, normalizeSeverity(out.Severity))
	out.SpecificProblem = firstNonEmpty(out.SpecificProblem, out.Message, def.SpecificProblem)
	out.Message = firstNonEmpty(out.Message, out.SpecificProblem)
	out.Severity = firstNonEmpty(out.Severity, strings.ToLower(out.PerceivedSeverity))
	return out
}

func canonicalCode(component, code string) string {
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "uds":
		return AlarmUDSDisconnected
	case "netconf":
		return AlarmNetconfDown
	}
	switch {
	case strings.Contains(code, "PROCESS"):
		return AlarmSrsENBProcessDown
	case strings.Contains(code, "NETWORK"):
		return AlarmS1Down
	default:
		return code
	}
}

// NewThresholdAlarm builds a normalized threshold-crossing alarm payload.
func NewThresholdAlarm(code, moi, problem string, value float64) domain.Alarm {
	return NewThresholdAlarmWithSeverity(code, moi, problem, value, "")
}

// NewThresholdAlarmWithSeverity builds a threshold-crossing alarm and optionally
// overrides the dictionary severity for rules that escalate dynamically.
func NewThresholdAlarmWithSeverity(code, moi, problem string, value float64, severity string) domain.Alarm {
	def, ok := Dictionary[code]
	if !ok {
		def = Dictionary[AlarmGenericEMS]
	}
	msg := problem
	if msg == "" {
		msg = def.SpecificProblem
	}
	perceived := def.PerceivedSeverity
	if normalized := normalizeSeverity(severity); normalized != "" {
		perceived = normalized
	}
	return domain.Alarm{
		Code:                  code,
		Message:               msg,
		AlarmID:               code,
		ManagedObjectInstance: moi,
		EventType:             def.EventType,
		ProbableCause:         def.ProbableCause,
		PerceivedSeverity:     perceived,
		SpecificProblem:       msg,
		Severity:              strings.ToLower(perceived),
	}
}

func normalizeSeverity(sev string) string {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical":
		return SeverityCritical
	case "major":
		return SeverityMajor
	case "minor":
		return SeverityMinor
	case "warning", "warn":
		return SeverityWarning
	case "cleared", "clear":
		return SeverityCleared
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
