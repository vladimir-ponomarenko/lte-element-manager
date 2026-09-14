package metrics

import (
	"bytes"
	"encoding/json"
	"errors"

	"lte-element-manager/internal/ems/domain"
)

var (
	ErrInvalidJSON = errors.New("invalid json")
	ErrInvalidType = errors.New("invalid metrics type")
	ErrMissingRoot = errors.New("missing required root fields")
)

func ParserFor(elementType domain.ElementType) ParseFunc {
	switch elementType {
	case domain.ElementENB:
		return func(raw []byte) (any, error) {
			return ParseEnbMetrics(raw)
		}
	case domain.ElementGNB, domain.ElementOAIGNB:
		return func(raw []byte) (any, error) { return ParseGnbMetrics(raw) }
	default:
		return nil
	}
}

// ParseGnbMetrics validates the vendor-neutral minimum NR telemetry contract.
// Vendor counters remain in NGAP/RRC maps so a newer srsRAN payload does not
// make the management plane discard the complete report.
func ParseGnbMetrics(raw []byte) (*domain.GnbMetrics, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m domain.GnbMetrics
	if err := dec.Decode(&m); err != nil {
		return nil, ErrInvalidJSON
	}
	if m.Type != "gnb_metrics" {
		return nil, ErrInvalidType
	}
	if m.Timestamp == 0 || m.Identity() == "" {
		return nil, ErrMissingRoot
	}
	return &m, nil
}

// ParseEnbMetrics validates and parses srsENB JSON payload into a typed struct.
func ParseEnbMetrics(raw []byte) (*domain.EnbMetrics, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var m domain.EnbMetrics
	if err := dec.Decode(&m); err != nil {
		return nil, ErrInvalidJSON
	}
	if m.Type != "enb_metrics" {
		return nil, ErrInvalidType
	}
	if m.Timestamp == 0 || m.EnbSerial == "" {
		return nil, ErrMissingRoot
	}
	return &m, nil
}
