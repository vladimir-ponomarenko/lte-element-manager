package srsran

import (
	"testing"

	"lte-element-manager/internal/ems/domain"
)

func TestNewMetricsSource(t *testing.T) {
	s, err := NewMetricsSource(domain.ElementENB, "/tmp/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := s.(*ENBMetricsReader); !ok {
		t.Fatalf("unexpected type")
	}

	if _, err := NewMetricsSource(domain.ElementEPC, ""); err == nil {
		t.Fatalf("expected error")
	}
	if _, err := NewMetricsSource(domain.ElementType("nope"), ""); err == nil {
		t.Fatalf("expected error")
	}
}
