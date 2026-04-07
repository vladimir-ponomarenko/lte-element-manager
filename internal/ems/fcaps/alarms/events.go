package alarms

import (
	"time"

	"lte-element-manager/internal/ems/domain"
)

// Event is published on the internal bus.
type Event struct {
	At        time.Time
	Component string
	Health    string
	Alarm     domain.Alarm
	Status    Status
	Count     uint64
}
