package wiring

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/config"
)

func TestContainer_Build_InvalidElement(t *testing.T) {
	cfg := config.Default()
	cfg.Element.Type = "nope"
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestContainer_Build_NetconfSSHIncomplete(t *testing.T) {
	cfg := config.Default()
	cfg.Netconf.Enabled = true
	cfg.Netconf.Transport = "ssh"
	cfg.Netconf.SnapshotPath = "x"
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestContainer_Build_NetconfSnapshotMissing(t *testing.T) {
	cfg := config.Default()
	cfg.Netconf.Enabled = true
	cfg.Netconf.Transport = "ssh"
	cfg.Netconf.SSH.HostKey = "x"
	cfg.Netconf.SSH.AuthorizedKey = "x"
	cfg.Netconf.SSH.Username = "u"
	cfg.Netconf.SnapshotPath = ""
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestContainer_Build_OK_WithTCPNetconf(t *testing.T) {
	cfg := config.Default()
	cfg.Netconf.Enabled = true
	cfg.Netconf.Transport = "tcp"
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestContainer_Build_ControlEnabledWithoutTargets(t *testing.T) {
	cfg := config.Default()
	cfg.Control.Enabled = true
	cfg.Control.Restart.Targets = nil
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestContainer_Build_ControlInvalidTimeout(t *testing.T) {
	cfg := config.Default()
	cfg.Control.Enabled = true
	cfg.Control.Restart.Timeout = "bad"
	cfg.Control.Restart.Targets = []config.ControlRestartTarget{
		{Serial: "s1", Container: "ENB-1"},
	}
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestContainer_Build_ControlTargetSerialFromConfig(t *testing.T) {
	dir := t.TempDir()
	enbPath := filepath.Join(dir, "enb.conf")
	if err := os.WriteFile(enbPath, []byte("[enb]\nn_prb = 50\n[expert]\nenb_serial = ENB-X\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := config.Default()
	cfg.Control.Enabled = true
	cfg.Control.Restart.Targets = []config.ControlRestartTarget{
		{Container: "ENB-1", ENBConfigPath: enbPath},
	}
	c := New(cfg, zerolog.Nop())
	if _, err := c.Build(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
