package telemetry

import "lte-element-manager/internal/ems/domain/canonical"

type Event struct {
	Samples []canonical.Sample
}
