package netconf

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	emserrors "lte-element-manager/internal/errors"
)

func TestProcessServer_EmptyBinary(t *testing.T) {
	p := &ProcessServer{}
	if p.Name() == "" {
		t.Fatalf("expected name")
	}
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestProcessServer_RunAndStop(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "proc.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
echo "NETCONF_GET user=u ts=0 json={\"x\":1}"
echo "[ERR]: bad" 1>&2
trap 'exit 0' TERM INT
while true; do sleep 1; done
`), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.DebugLevel)

	ctx, cancel := context.WithCancel(context.Background())
	p := &ProcessServer{
		Binary:        script,
		Addr:          "127.0.0.1:0",
		YangDir:       ".",
		SnapshotPath:  "x",
		HostKey:       "x",
		AuthorizedKey: "x",
		Username:      "u",
		Log:           log,
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

	out := buf.String()
	if !strings.Contains(out, "netconf ssh server started") {
		t.Fatalf("missing start log: %s", out)
	}
}

func TestProcessServer_ExitError(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

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

	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeProcess {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
}
