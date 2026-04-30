package alarms

import "time"

// Status indicates lifecycle state of an alarm instance.
type Status string

const (
	StatusActive  Status = "active"
	StatusCleared Status = "cleared"
)

const (
	EventTypeCommunicationsAlarm    = "CommunicationsAlarm"
	EventTypeProcessingErrorAlarm   = "ProcessingErrorAlarm"
	EventTypeQualityOfServiceAlarm  = "QualityOfServiceAlarm"
	EventTypeEquipmentAlarm         = "EquipmentAlarm"
	SeverityCritical                = "CRITICAL"
	SeverityMajor                   = "MAJOR"
	SeverityMinor                   = "MINOR"
	SeverityWarning                 = "WARNING"
	SeverityCleared                 = "CLEARED"
	ProbableCauseLANError           = "LAN_ERROR"
	ProbableCauseSoftwareError      = "SOFTWARE_ERROR"
	ProbableCauseThresholdCrossed   = "THRESHOLD_CROSSED"
	ProbableCauseEquipmentMalfunc   = "EQUIPMENT_MALFUNCTION"
	ProbableCauseCommunicationsFail = "COMMUNICATIONS_FAILURE"
)

// Key uniquely identifies an alarm stream for deduplication.
type Key struct {
	Component string
	Code      string
}

// Record is the in-memory representation of an alarm instance.
type Record struct {
	Key                   Key
	AlarmID               string
	ManagedObjectInstance string
	EventType             string
	ProbableCause         string
	PerceivedSeverity     string
	SpecificProblem       string
	Message               string
	Severity              string
	Status                Status
	FirstSeen             time.Time
	LastSeen              time.Time
	Count                 uint64
}
