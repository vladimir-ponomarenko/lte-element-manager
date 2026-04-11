package pm

import (
	"time"

	"lte-element-manager/internal/ems/domain/canonical"
	"lte-element-manager/internal/ems/domain/nrm"
)

type Config struct {
	Granularity time.Duration
	ReportEvery time.Duration
}

type ConfigUpdate struct {
	Config Config
}

type Value struct {
	Metric canonical.Metric
	Value  float64
}

type Report struct {
	Begin       time.Time
	End         time.Time
	Granularity time.Duration

	ByDN map[nrm.DN]map[string]Value
}

type Event struct {
	Report Report
}
