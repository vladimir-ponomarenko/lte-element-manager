package mediation

import "lte-element-manager/internal/ems/domain/canonical"

type Mapper interface {
	Map(rawJSON string) ([]canonical.Sample, error)
}
