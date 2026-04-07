package netconf

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestToStringAndFormatFloat(t *testing.T) {
	if toString(true) != "true" || toString(false) != "false" {
		t.Fatalf("unexpected bool conversion")
	}
	if toString("x") != "x" {
		t.Fatalf("unexpected string conversion")
	}
	if toString(float64(1)) != "1" {
		t.Fatalf("unexpected int-float conversion: %q", toString(float64(1)))
	}
	s := toString(float64(1.25))
	if !strings.Contains(s, ".") {
		t.Fatalf("unexpected float conversion: %q", s)
	}
}

func TestEncodeElement_ArrayAndMap(t *testing.T) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)

	if err := encodeElement(enc, "root", []any{
		map[string]any{"k": "v"},
		"plain",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "<root>") {
		t.Fatalf("missing root: %s", out)
	}
	if !strings.Contains(out, "<item><k>v</k></item>") {
		t.Fatalf("missing map item: %s", out)
	}
	if !strings.Contains(out, "<item>plain</item>") {
		t.Fatalf("missing plain item: %s", out)
	}
}
