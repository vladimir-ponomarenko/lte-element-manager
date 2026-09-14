package nrm

import (
	"lte-element-manager/internal/ems/domain/canonical"
	"strings"
	"testing"
)

func TestRegistry_GNBResolvesNRCell(t *testing.T) {
	r, err := New(Config{SubNetwork: "s", ManagedElement: "g", GNBFunctionID: "1", ElementType: "gnb"})
	if err != nil {
		t.Fatal(err)
	}
	dn, err := r.Resolve(canonical.Sample{Scope: "cell:nr=123/ue:rnti=1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dn), "GNBCUCPFunction=1,NRCellDU=nr-123") {
		t.Fatalf("DN = %s", dn)
	}
}

func TestRegistry_GNBRequiresGNBFunctionID(t *testing.T) {
	_, err := New(Config{SubNetwork: "s", ManagedElement: "g", ENBFunctionID: "legacy", ElementType: "gnb"})
	if err == nil {
		t.Fatal("gNB registry accepted an LTE function ID")
	}
}
