package logging

import (
	"testing"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/config"
)

func TestNew_ParseLevelBranches(t *testing.T) {
	cfg := config.LogConfig{
		Level:     "nope",
		Format:    "json",
		Color:     false,
		Timestamp: true,
	}
	log := New(cfg)
	if log.GetLevel() != zerolog.InfoLevel {
		t.Fatalf("expected info level on invalid input, got %s", log.GetLevel())
	}

	cfg.Level = ""
	log = New(cfg)
	if log.GetLevel() != zerolog.InfoLevel {
		t.Fatalf("expected info level on empty, got %s", log.GetLevel())
	}
}

func TestWithComponent_OverridesAndInvalid(t *testing.T) {
	base := zerolog.Nop()
	cfg := config.LogConfig{
		Level:      "info",
		Components: map[string]string{"netconf": "debug", "bad": "nope"},
	}

	net := WithComponent(base, cfg, "netconf")
	if net.GetLevel() != zerolog.DebugLevel {
		t.Fatalf("expected debug, got %s", net.GetLevel())
	}
	bad := WithComponent(base, cfg, "bad")
	if bad.GetLevel() != base.GetLevel() {
		t.Fatalf("expected fallback level")
	}
	other := WithComponent(base, cfg, "other")
	if other.GetLevel() != base.GetLevel() {
		t.Fatalf("expected base level")
	}
}

func TestLevelStyle_AllCases(t *testing.T) {
	c, s := levelStyle("error")
	if c == "" || s != "ERR" {
		t.Fatalf("unexpected: %q %q", c, s)
	}
	_, s = levelStyle("fatal")
	if s != "ERR" {
		t.Fatalf("unexpected: %q", s)
	}
	_, s = levelStyle("panic")
	if s != "ERR" {
		t.Fatalf("unexpected: %q", s)
	}
	_, s = levelStyle("warn")
	if s != "WRN" {
		t.Fatalf("unexpected: %q", s)
	}
	_, s = levelStyle("info")
	if s != "INF" {
		t.Fatalf("unexpected: %q", s)
	}
	_, s = levelStyle("debug")
	if s != "DBG" {
		t.Fatalf("unexpected: %q", s)
	}
	_, s = levelStyle("trace")
	if s != "TRC" {
		t.Fatalf("unexpected: %q", s)
	}
	_, s = levelStyle("custom")
	if s != "CUSTOM" {
		t.Fatalf("unexpected: %q", s)
	}
}

func TestNew_ConsoleColoredBranch(t *testing.T) {
	cfg := config.LogConfig{
		Level:     "info",
		Format:    "console",
		Color:     true,
		Timestamp: false,
	}
	_ = New(cfg)
}
