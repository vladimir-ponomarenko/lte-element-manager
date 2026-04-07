package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_SelfCheck(t *testing.T) {
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run(context.Background(), []string{"--self-check"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if out.String() == "" {
		t.Fatalf("expected output")
	}
}

func TestRun_ConfigError(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run(context.Background(), []string{"--config", dir}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected non-zero exit code, got %d", code)
	}
}

func TestRun_WiringErrorDoesNotFailHard(t *testing.T) {
	cfg := []byte("element:\n  type: nope\n")
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, cfg, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out bytes.Buffer
	var errBuf bytes.Buffer
	code := run(context.Background(), []string{"--config", path}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("expected 0 exit code to preserve behavior, got %d", code)
	}
}

func TestRun_FlagParseError(t *testing.T) {
	var out bytes.Buffer
	var errBuf bytes.Buffer

	code := run(context.Background(), []string{"--nope"}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRun_RunnerErrorPath(t *testing.T) {
	// Force a fast failure in the metrics reader by providing an empty socket path.
	cfg := []byte("element:\n  type: enb\n  socket_path: \"\"\n")
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, cfg, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out bytes.Buffer
	var errBuf bytes.Buffer
	code := run(context.Background(), []string{"--config", path}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("expected 0 exit code to preserve behavior, got %d", code)
	}
}
