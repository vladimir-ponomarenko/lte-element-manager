package canonical

// MetricType drives future PM aggregation semantics.
type MetricType string

const (
	Counter MetricType = "COUNTER"
	Gauge   MetricType = "GAUGE"
)

// Metric is a numeric measurement.
type Metric struct {
	Name  string
	Type  MetricType
	Value float64
	Unit  string
}

// Sample is a canonical, vendor-agnostic telemetry sample.
type Sample struct {
	Timestamp int64
	SourceID  string
	Scope     string

	Metrics map[string]Metric
	Attrs   map[string]string
}
