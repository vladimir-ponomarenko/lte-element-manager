package domain

type ElementType string

const (
	ElementENB ElementType = "enb"
	ElementEPC ElementType = "epc"
)

type MetricSample struct {
	RawJSON string
	Parsed  any
}

type Alarm struct {
	Code                  string
	Message               string
	Severity              string
	AlarmID               string
	ManagedObjectInstance string
	EventType             string
	ProbableCause         string
	PerceivedSeverity     string
	SpecificProblem       string
}

type Command struct {
	Name string
	Args map[string]string
}
