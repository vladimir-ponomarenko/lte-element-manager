package domain

type ElementType string

const (
	ElementENB ElementType = "enb"
	// ElementGNB is a 5G NR gNodeB.  Keep it separate from ENB: the core
	// interface (NGAP) and the NRM hierarchy are different.
	ElementGNB ElementType = "gnb"
	// ElementOAIGNB is an OpenAirInterface 5G gNB. Its metrics normally come
	// from an E2/KPM bridge, not from the srsRAN metrics exporter.
	ElementOAIGNB ElementType = "oai-gnb"
	ElementEPC    ElementType = "epc"
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
