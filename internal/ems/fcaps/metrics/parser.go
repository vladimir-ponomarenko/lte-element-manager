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
	default:
		return nil
	}
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
