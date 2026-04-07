package netconf

import "testing"

func TestEnabled_Default(t *testing.T) {
	if Enabled() {
		t.Fatalf("expected disabled without build tag")
	}
}
