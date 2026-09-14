package srsran

import (
	"context"

	"lte-element-manager/internal/ems/domain"
)

// OAI5GMetricsReader receives datagrams from an OAI E2/KPM-to-JSON bridge.
// OAI itself exposes KPM through its E2 agent; it does not provide the srsRAN
// metrics UDS protocol. The bridge must emit the validated gnb_metrics
// envelope consumed by the common NR FCAPS pipeline.
type OAI5GMetricsReader struct {
	SocketPath string
}

func (r *OAI5GMetricsReader) Run(ctx context.Context, out chan<- domain.MetricSample) error {
	return (&ENBMetricsReader{SocketPath: r.SocketPath}).Run(ctx, out)
}
