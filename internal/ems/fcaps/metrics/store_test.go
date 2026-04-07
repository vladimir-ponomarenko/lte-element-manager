package metrics

import (
	"testing"

	"lte-element-manager/internal/ems/domain"
)

func TestStoreLatest(t *testing.T) {
	s := NewStore()
	s.Update(domain.MetricSample{RawJSON: `{"x":1}`})
	if s.Latest().RawJSON == "" {
		t.Fatalf("expected latest")
	}
}
