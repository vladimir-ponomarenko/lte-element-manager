package netconf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	emserrors "lte-element-manager/internal/errors"
)

type errReader struct {
	once bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.once {
		return 0, errors.New("boom")
	}
	r.once = true
	return copy(p, "line\n"), nil
}

func TestScanNetconfOutput_LogsScannerErrInDebug(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.DebugLevel)
	scanNetconfOutput(&errReader{}, log)
	if !strings.Contains(buf.String(), "netconf stdout scan failed") {
		t.Fatalf("expected scanner error log, got: %s", buf.String())
	}
}

func TestScanNetconfErrors_LogsScannerErrInDebug(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.DebugLevel)
	scanNetconfErrors(&errReader{}, log)
	if !strings.Contains(buf.String(), "netconf stderr scan failed") {
		t.Fatalf("expected scanner error log, got: %s", buf.String())
	}
}

func TestProcessServer_StdoutPipeError(t *testing.T) {
	prev := execCommand
	execCommand = func(string, ...string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", "exit 0")
		cmd.Stdout = os.Stdout
		return cmd
	}
	defer func() { execCommand = prev }()

	p := &ProcessServer{
		Binary:        "ignored",
		Addr:          "127.0.0.1:0",
		YangDir:       ".",
		SnapshotPath:  "x",
		HostKey:       "x",
		AuthorizedKey: "x",
		Username:      "u",
		Log:           zerolog.Nop(),
	}
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeProcess {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
}

func TestProcessServer_StderrPipeError(t *testing.T) {
	prev := execCommand
	execCommand = func(string, ...string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", "exit 0")
		cmd.Stderr = os.Stderr
		return cmd
	}
	defer func() { execCommand = prev }()

	p := &ProcessServer{
		Binary:        "ignored",
		Addr:          "127.0.0.1:0",
		YangDir:       ".",
		SnapshotPath:  "x",
		HostKey:       "x",
		AuthorizedKey: "x",
		Username:      "u",
		Log:           zerolog.Nop(),
	}
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeProcess {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
}

func TestProcessServer_KillTimeoutBranch(t *testing.T) {
	prevTimeout := processKillTimeout
	processKillTimeout = 20 * time.Millisecond
	defer func() { processKillTimeout = prevTimeout }()

	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
trap '' TERM
while true; do sleep 1; done
`), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &ProcessServer{
		Binary:        script,
		Addr:          "127.0.0.1:0",
		YangDir:       ".",
		SnapshotPath:  "x",
		HostKey:       "x",
		AuthorizedKey: "x",
		Username:      "u",
		Log:           zerolog.Nop(),
	}

	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}
