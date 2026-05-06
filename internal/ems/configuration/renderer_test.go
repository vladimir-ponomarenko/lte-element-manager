package configuration

import (
	"strings"
	"testing"
)

func TestPatchLibconfigScalar_SemicolonlessLineDoesNotConsumeNextAssignment(t *testing.T) {
	in := []byte("sib1 = {\n  cell_barred = \"NotBarred\"\n  si_window_length = 20;\n};\n")
	got, err := patchLibconfigScalar(in, "cell_barred", `"Barred"`)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "cell_barred = \"Barred\"\n") {
		t.Fatalf("cell_barred not patched correctly: %s", out)
	}
	if !strings.Contains(out, "si_window_length = 20;") {
		t.Fatalf("next assignment was damaged: %s", out)
	}
}

func TestPatchLibconfigScalar_IgnoresCommentsAndStrings(t *testing.T) {
	in := []byte("// pci = 99;\nname = \"pci = 88;\";\npci = 1;\n")
	got, err := patchLibconfigScalar(in, "pci", "2")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "// pci = 99;") || !strings.Contains(out, `name = "pci = 88;"`) {
		t.Fatalf("comment/string content was modified: %s", out)
	}
	if !strings.Contains(out, "pci = 2;") {
		t.Fatalf("real assignment was not patched: %s", out)
	}
}

func TestPatchLibconfigScalar_DoesNotTreatURLAsComment(t *testing.T) {
	in := []byte("device_args = tx_port=tcp://*:2000,rx_port=tcp://srsue:2001;\npci = 1;\n")
	got, err := patchLibconfigScalar(in, "pci", "2")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "tx_port=tcp://*:2000,rx_port=tcp://srsue:2001;") {
		t.Fatalf("URL-like scalar was comment-truncated: %s", out)
	}
	if !strings.Contains(out, "pci = 2;") {
		t.Fatalf("real assignment was not patched: %s", out)
	}
}
