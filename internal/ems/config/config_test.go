package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFile_UsesDefaultsAndEnvOverrides(t *testing.T) {
	t.Setenv("EMS_ELEMENT_TYPE", "enb")
	t.Setenv("EMS_BUS_BUFFER", "321")
	t.Setenv("EMS_LOG_LEVEL", "debug")
	t.Setenv("EMS_LOG_COLOR", "true")
	t.Setenv("EMS_NETCONF_ENABLED", "1")
	t.Setenv("EMS_NETCONF_ADDR", "0.0.0.0:8301")
	t.Setenv("EMS_NETCONF_SSH_USERNAME", "nms")

	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bus.Buffer != 321 {
		t.Fatalf("bus buffer override not applied: %d", cfg.Bus.Buffer)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("log level override not applied: %s", cfg.Log.Level)
	}
	if !cfg.Log.Color {
		t.Fatalf("log color override not applied")
	}
	if !cfg.Netconf.Enabled || cfg.Netconf.Addr != "0.0.0.0:8301" {
		t.Fatalf("netconf overrides not applied: %+v", cfg.Netconf)
	}
	if cfg.Netconf.SSH.Username != "nms" {
		t.Fatalf("ssh username override not applied: %s", cfg.Netconf.SSH.Username)
	}
}

func TestLoad_FilePartial_AppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	data := []byte(`
element:
  type: enb
log:
  format: console
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bus.Buffer == 0 {
		t.Fatalf("expected default bus buffer")
	}
	if cfg.Log.Components == nil {
		t.Fatalf("expected components map to be initialized")
	}
	if cfg.Netconf.SSH.Username == "" {
		t.Fatalf("expected default ssh username")
	}
}

func TestEnvBool_InvalidDoesNotOverride(t *testing.T) {
	t.Setenv("EMS_LOG_COLOR", "maybe")
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default is false.
	if cfg.Log.Color {
		t.Fatalf("expected default value to remain")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte(":"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEnvInt_InvalidDoesNotOverride(t *testing.T) {
	t.Setenv("EMS_BUS_BUFFER", "abc")
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bus.Buffer != Default().Bus.Buffer {
		t.Fatalf("expected default buffer, got %d", cfg.Bus.Buffer)
	}
}

func TestEnvString_FallbackKeys(t *testing.T) {
	t.Setenv("SOCKET_PATH", "/tmp/x.uds")
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Element.SocketPath != "/tmp/x.uds" {
		t.Fatalf("expected fallback env key to work, got %s", cfg.Element.SocketPath)
	}
}

func TestLoad_UsesEMSConfigEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("element:\n  type: enb\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EMS_CONFIG", path)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Element.Type != "enb" {
		t.Fatalf("unexpected element type: %s", cfg.Element.Type)
	}
}

func TestEnvOverrides_MoreKeys(t *testing.T) {
	t.Setenv("EMS_NETCONF_TRANSPORT", "ssh")
	t.Setenv("EMS_NETCONF_SNAPSHOT_PATH", "/tmp/snap.json")
	t.Setenv("EMS_NETCONF_YANG_DIR", "/tmp/yang")
	t.Setenv("EMS_NETCONF_SSH_HOSTKEY", "/tmp/hostkey")
	t.Setenv("EMS_NETCONF_SSH_AUTHORIZED_KEY", "/tmp/auth")
	t.Setenv("EMS_LOG_TIMESTAMP", "0")
	t.Setenv("EMS_LOG_COLOR", "FALSE")

	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Netconf.Transport != "ssh" || cfg.Netconf.SnapshotPath != "/tmp/snap.json" || cfg.Netconf.YangDir != "/tmp/yang" {
		t.Fatalf("unexpected netconf: %+v", cfg.Netconf)
	}
	if cfg.Netconf.SSH.HostKey != "/tmp/hostkey" || cfg.Netconf.SSH.AuthorizedKey != "/tmp/auth" {
		t.Fatalf("unexpected ssh: %+v", cfg.Netconf.SSH)
	}
	if cfg.Log.Timestamp {
		t.Fatalf("expected timestamp override false")
	}
	if cfg.Log.Color {
		t.Fatalf("expected color override false")
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("K1", "")
	t.Setenv("K2", "v2")
	if got := envString("K1", "K2", "K3"); got != "v2" {
		t.Fatalf("unexpected envString: %q", got)
	}
	if got := envString("NOPE1", "NOPE2"); got != "" {
		t.Fatalf("expected empty")
	}

	t.Setenv("B1", "YES")
	if v, ok := envBool("B1"); !ok || !v {
		t.Fatalf("unexpected envBool yes: %v %v", v, ok)
	}
	t.Setenv("B2", "n")
	if v, ok := envBool("B2"); !ok || v {
		t.Fatalf("unexpected envBool no: %v %v", v, ok)
	}

	t.Setenv("I1", "10")
	if v, ok := envInt("I1"); !ok || v != 10 {
		t.Fatalf("unexpected envInt: %d %v", v, ok)
	}
	if _, ok := envInt("NOPE_INT"); ok {
		t.Fatalf("expected false")
	}
}
