package srsran

import (
	"context"
	"errors"

	"lte-element-manager/internal/ems/domain"
)

var ErrUnsupported = errors.New("unsupported element type")

// MetricsSource is the adapter contract used by the EMS application layer.
type MetricsSource interface {
	Run(ctx context.Context, out chan<- domain.MetricSample) error
}

func NewMetricsSource(elementType domain.ElementType, socketPath string) (MetricsSource, error) {
	switch elementType {
	case domain.ElementENB:
		return &ENBMetricsReader{SocketPath: socketPath}, nil
	case domain.ElementEPC:
		return nil, ErrUnsupported
	default:
		return nil, ErrUnsupported
	}
}
