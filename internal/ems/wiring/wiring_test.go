package wiring

import (
	"context"
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
