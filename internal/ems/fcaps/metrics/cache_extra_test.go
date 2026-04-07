package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSnapshot_Error(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.MkdirAll(ro, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(ro, "x", "snap.json")
	if err := writeSnapshot(path, `{"x":1}`); err == nil {
		t.Fatalf("expected error")
	}
}

func TestAsMap(t *testing.T) {
	if _, ok := asMap(map[string]any{"x": 1}); !ok {
		t.Fatalf("expected ok")
	}
	if _, ok := asMap(123); ok {
		t.Fatalf("expected false")
	}
}
