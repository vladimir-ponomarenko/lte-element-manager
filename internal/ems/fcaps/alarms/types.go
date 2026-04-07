package alarms

import "time"

// Status indicates lifecycle state of an alarm instance.
type Status string

const (
	StatusActive  Status = "active"
	StatusCleared Status = "cleared"
)

// Key uniquely identifies an alarm stream for deduplication.
type Key struct {
	Component string
	Code      string
}

// Record is the in-memory representation of an alarm instance.
type Record struct {
	Key       Key
	Message   string
	Severity  string
	Status    Status
	FirstSeen time.Time
	LastSeen  time.Time
	Count     uint64
}
