package alarms

import (
	"fmt"
	"strings"

	"lte-element-manager/internal/ems/domain"
)

const (
	AlarmS1Down            = "Alarm_S1_Down"
	AlarmUDSDisconnected   = "Alarm_UDS_Disconnected"
	AlarmSrsENBProcessDown = "Alarm_SrsENB_ProcessDown"
	AlarmHighBLER          = "Alarm_High_BLER"
	AlarmLowThroughput     = "Alarm_Low_Throughput"
	AlarmNetconfDown       = "Alarm_Netconf_Down"
	AlarmGenericEMS        = "Alarm_EMS_Generic"
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

func NewThresholdAlarm(code, moi, problem string, value float64) domain.Alarm {
	def, ok := Dictionary[code]
	if !ok {
		def = Dictionary[AlarmGenericEMS]
	}
	msg := problem
	if msg == "" {
		msg = def.SpecificProblem
	}
	return domain.Alarm{
		Code:                  code,
		Message:               fmt.Sprintf("%s (value %.6f)", msg, value),
		AlarmID:               code,
		ManagedObjectInstance: moi,
		EventType:             def.EventType,
		ProbableCause:         def.ProbableCause,
		PerceivedSeverity:     def.PerceivedSeverity,
		SpecificProblem:       msg,
		Severity:              strings.ToLower(def.PerceivedSeverity),
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
