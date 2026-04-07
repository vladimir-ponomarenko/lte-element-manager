package netconf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	emserrors "lte-element-manager/internal/errors"
)

func TestScanNetconfOutput_DebugGate(t *testing.T) {
	in := "hello\nNETCONF_GET user=u ts=1 json={\"x\":1}\n"

	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.InfoLevel)
	scanNetconfOutput(strings.NewReader(in), log)
	out := buf.String()
	if strings.Contains(out, "hello") {
		t.Fatalf("unexpected debug output: %s", out)
	}
	if !strings.Contains(out, "netconf_get") {
		t.Fatalf("missing get log: %s", out)
	}

	buf.Reset()
	log = zerolog.New(&buf).Level(zerolog.DebugLevel)
	scanNetconfOutput(strings.NewReader("hello\n"), log)
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("expected debug output")
	}
}

func TestScanNetconfErrors_ErrorClassification(t *testing.T) {
	in := "fine\n[ERR]: bad\nsomething error happened\n"

	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.InfoLevel)
	scanNetconfErrors(strings.NewReader(in), log)
	out := buf.String()
	if !strings.Contains(out, "[ERR]") || !strings.Contains(out, "something error happened") {
		t.Fatalf("expected errors to be logged: %s", out)
	}
	if strings.Contains(out, "fine") {
		t.Fatalf("did not expect non-error line to be logged: %s", out)
	}
}

func TestEmitNetconfGetLog_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.DebugLevel)
	emitNetconfGetLog("NETCONF_GET user=u ts=1", log)
	if !strings.Contains(buf.String(), "NETCONF_GET user=u ts=1") {
		t.Fatalf("expected debug log to contain raw line: %s", buf.String())
	}
}

func TestProcessServer_StartErrorWrapped(t *testing.T) {
	p := &ProcessServer{
		Binary:        "/no/such/binary",
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
