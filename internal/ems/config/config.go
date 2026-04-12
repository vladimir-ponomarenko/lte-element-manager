package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Element defines which network element this EMS instance manages.
	Element ElementConfig `yaml:"element"`
	// Bus controls the internal message bus settings.
	Bus BusConfig `yaml:"bus"`
	// Log controls logging format and verbosity.
	Log LogConfig `yaml:"log"`
	// Metrics controls EMS metrics handling.
	Metrics MetricsConfig `yaml:"metrics"`
	// Netconf controls NETCONF server settings.
	Netconf NetconfConfig `yaml:"netconf"`
	// NRM controls managed-object topology settings.
	NRM NRMConfig `yaml:"nrm"`
	// PM controls performance management engine settings.
	PM PMConfig `yaml:"pm"`
	// Control configures control-plane APIs (configuration actions).
	Control ControlConfig `yaml:"control"`
}

type ElementConfig struct {
	// Type is the network element type: enb, epc, etc.
	Type string `yaml:"type"`
	// SocketPath is the UDS path for JSON metrics from the element.
	SocketPath string `yaml:"socket_path"`
}

type BusConfig struct {
	// Buffer is the size of the internal bus channel buffer.
	Buffer int `yaml:"buffer"`
}

type LogConfig struct {
	// Level is the default log level (trace, debug, info, warn, error, fatal, panic).
	Level string `yaml:"level"`
	// Format is log output format: json or console.
	Format string `yaml:"format"`
	// Color enables colored levels in console format.
	Color bool `yaml:"color"`
	// Timestamp enables RFC3339 timestamps.
	Timestamp bool `yaml:"timestamp"`
	// Components overrides log levels per component name.
	Components map[string]string `yaml:"components"`
}

type MetricsConfig struct {
	// SnapshotPath stores the latest raw JSON metrics for NETCONF/NMS reads.
	SnapshotPath string `yaml:"snapshot_path"`
	// LogUDS enables raw UDS metrics logging.
	LogUDS bool `yaml:"log_uds_metrics"`
}

type NetconfConfig struct {
	// Enabled toggles the NETCONF server.
	Enabled bool `yaml:"enabled"`
	// Addr is the listen address (e.g., 0.0.0.0:8301).
	Addr string `yaml:"addr"`
	// Transport is a placeholder for future SSH/TLS support; currently "tcp".
	Transport string `yaml:"transport"`
	// SnapshotPath is the file path from which NETCONF reads the latest metrics.
	SnapshotPath string `yaml:"snapshot_path"`
	// YangDir is the directory containing YANG models to load.
	YangDir string `yaml:"yang_dir"`
	// SSH settings (used when transport is ssh).
	SSH NetconfSSHConfig `yaml:"ssh"`
}

type NetconfSSHConfig struct {
	// HostKey is the server private key path.
	HostKey string `yaml:"hostkey"`
	// AuthorizedKey is the public key path for user authentication.
	AuthorizedKey string `yaml:"authorized_key"`
	// Username allowed to authenticate.
	Username string `yaml:"username"`
}

type NRMConfig struct {
	SubNetwork     string `yaml:"subnetwork"`
	ManagedElement string `yaml:"managed_element"`
	ENBFunctionID  string `yaml:"enb_function_id"`
}

type PMConfig struct {
	Enabled           bool   `yaml:"enabled"`
	GranularityPeriod string `yaml:"granularity_period"`
	ReportPeriod      string `yaml:"report_period"`
}

type ControlConfig struct {
	Enabled bool           `yaml:"enabled"`
	Addr    string         `yaml:"addr"`
	Restart ControlRestart `yaml:"restart"`
}

type ControlRestart struct {
	DockerSocket string                 `yaml:"docker_socket"`
	Timeout      string                 `yaml:"timeout"`
	Targets      []ControlRestartTarget `yaml:"targets"`
}

type ControlRestartTarget struct {
	Serial          string   `yaml:"serial"`
	Container       string   `yaml:"container"`
	ENBConfigPath   string   `yaml:"enb_config_path"`
	RRConfigPath    string   `yaml:"rr_config_path"`
	Dependents      []string `yaml:"dependents"`
	DelayAfterStart string   `yaml:"delay_after_start"`
}

func Default() Config {
	return Config{
		Element: ElementConfig{
			Type:       "enb",
			SocketPath: "/var/run/enb-metrics/enb_metrics.uds",
		},
		Bus: BusConfig{
			Buffer: 200,
		},
		Log: LogConfig{
			Level:     "info",
			Format:    "json",
			Color:     false,
			Timestamp: true,
		},
		Metrics: MetricsConfig{
			SnapshotPath: "",
			LogUDS:       false,
		},
		Netconf: NetconfConfig{
			Enabled:      false,
			Addr:         "0.0.0.0:8300",
			Transport:    "tcp",
			SnapshotPath: "",
			YangDir:      "yang",
			SSH: NetconfSSHConfig{
				HostKey:       "",
				AuthorizedKey: "",
				Username:      "admin",
			},
		},
		NRM: NRMConfig{
			SubNetwork:     "srsRAN",
			ManagedElement: "enb1",
			ENBFunctionID:  "1",
		},
		PM: PMConfig{
			Enabled:           false,
			GranularityPeriod: "10s",
			ReportPeriod:      "10s",
		},
		Control: ControlConfig{
			Enabled: false,
			Addr:    "0.0.0.0:18080",
			Restart: ControlRestart{
				DockerSocket: "/var/run/docker.sock",
				Timeout:      "20s",
				Targets:      nil,
			},
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	if path == "" {
		if env := os.Getenv("EMS_CONFIG"); env != "" {
			path = env
		} else {
			path = "config/ems-agent/ems-agent.yaml"
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyDefaults(&cfg)
			applyEnvOverrides(&cfg)
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	applyDefaults(&cfg)
	applyEnvOverrides(&cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	def := Default()
	if cfg.Element.Type == "" {
		cfg.Element.Type = def.Element.Type
	}
	if cfg.Element.SocketPath == "" {
		cfg.Element.SocketPath = def.Element.SocketPath
	}
	if cfg.Bus.Buffer == 0 {
		cfg.Bus.Buffer = def.Bus.Buffer
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = def.Log.Level
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = def.Log.Format
	}
	if cfg.Log.Components == nil {
		cfg.Log.Components = map[string]string{}
	}
	if cfg.Metrics.SnapshotPath == "" {
		cfg.Metrics.SnapshotPath = def.Metrics.SnapshotPath
	}
	if cfg.Netconf.Addr == "" {
		cfg.Netconf.Addr = def.Netconf.Addr
	}
	if cfg.Netconf.Transport == "" {
		cfg.Netconf.Transport = def.Netconf.Transport
	}
	if cfg.Netconf.SnapshotPath == "" {
		cfg.Netconf.SnapshotPath = def.Netconf.SnapshotPath
	}
	if cfg.Netconf.YangDir == "" {
		cfg.Netconf.YangDir = def.Netconf.YangDir
	}
	if cfg.Netconf.SSH.Username == "" {
		cfg.Netconf.SSH.Username = def.Netconf.SSH.Username
	}
	if cfg.NRM.SubNetwork == "" {
		cfg.NRM.SubNetwork = def.NRM.SubNetwork
	}
	if cfg.NRM.ManagedElement == "" {
		cfg.NRM.ManagedElement = def.NRM.ManagedElement
	}
	if cfg.NRM.ENBFunctionID == "" {
		cfg.NRM.ENBFunctionID = def.NRM.ENBFunctionID
	}
	if cfg.PM.GranularityPeriod == "" {
		cfg.PM.GranularityPeriod = def.PM.GranularityPeriod
	}
	if cfg.PM.ReportPeriod == "" {
		cfg.PM.ReportPeriod = def.PM.ReportPeriod
	}
	if cfg.Control.Addr == "" {
		cfg.Control.Addr = def.Control.Addr
	}
	if cfg.Control.Restart.DockerSocket == "" {
		cfg.Control.Restart.DockerSocket = def.Control.Restart.DockerSocket
	}
	if cfg.Control.Restart.Timeout == "" {
		cfg.Control.Restart.Timeout = def.Control.Restart.Timeout
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := envString("EMS_ELEMENT_TYPE", "ELEMENT_TYPE"); v != "" {
		cfg.Element.Type = v
	}
	if v := envString("EMS_SOCKET_PATH", "SOCKET_PATH"); v != "" {
		cfg.Element.SocketPath = v
	}
	if v, ok := envInt("EMS_BUS_BUFFER", "BUS_BUFFER"); ok {
		cfg.Bus.Buffer = v
	}
	if v := envString("EMS_LOG_LEVEL", "LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := envString("EMS_LOG_FORMAT", "LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v, ok := envBool("EMS_LOG_COLOR", "LOG_COLOR"); ok {
		cfg.Log.Color = v
	}
	if v, ok := envBool("EMS_LOG_TIMESTAMP", "LOG_TIMESTAMP"); ok {
		cfg.Log.Timestamp = v
	}
	if v := envString("EMS_METRICS_SNAPSHOT_PATH", "METRICS_SNAPSHOT_PATH"); v != "" {
		cfg.Metrics.SnapshotPath = v
	}
	if v, ok := envBool("EMS_METRICS_LOG_UDS", "METRICS_LOG_UDS"); ok {
		cfg.Metrics.LogUDS = v
	}
	if v, ok := envBool("EMS_NETCONF_ENABLED", "NETCONF_ENABLED"); ok {
		cfg.Netconf.Enabled = v
	}
	if v := envString("EMS_NETCONF_ADDR", "NETCONF_ADDR"); v != "" {
		cfg.Netconf.Addr = v
	}
	if v := envString("EMS_NETCONF_TRANSPORT", "NETCONF_TRANSPORT"); v != "" {
		cfg.Netconf.Transport = v
	}
	if v := envString("EMS_NETCONF_SNAPSHOT_PATH", "NETCONF_SNAPSHOT_PATH"); v != "" {
		cfg.Netconf.SnapshotPath = v
	}
	if v := envString("EMS_NETCONF_YANG_DIR", "NETCONF_YANG_DIR"); v != "" {
		cfg.Netconf.YangDir = v
	}
	if v := envString("EMS_NETCONF_SSH_HOSTKEY", "NETCONF_SSH_HOSTKEY"); v != "" {
		cfg.Netconf.SSH.HostKey = v
	}
	if v := envString("EMS_NETCONF_SSH_AUTHORIZED_KEY", "NETCONF_SSH_AUTHORIZED_KEY"); v != "" {
		cfg.Netconf.SSH.AuthorizedKey = v
	}
	if v := envString("EMS_NETCONF_SSH_USERNAME", "NETCONF_SSH_USERNAME"); v != "" {
		cfg.Netconf.SSH.Username = v
	}
	if v := envString("EMS_NRM_SUBNETWORK", "NRM_SUBNETWORK"); v != "" {
		cfg.NRM.SubNetwork = v
	}
	if v := envString("EMS_NRM_MANAGED_ELEMENT", "NRM_MANAGED_ELEMENT"); v != "" {
		cfg.NRM.ManagedElement = v
	}
	if v := envString("EMS_NRM_ENB_FUNCTION_ID", "NRM_ENB_FUNCTION_ID"); v != "" {
		cfg.NRM.ENBFunctionID = v
	}
	if v, ok := envBool("EMS_PM_ENABLED", "PM_ENABLED"); ok {
		cfg.PM.Enabled = v
	}
	if v := envString("EMS_PM_GRANULARITY_PERIOD", "PM_GRANULARITY_PERIOD"); v != "" {
		cfg.PM.GranularityPeriod = v
	}
	if v := envString("EMS_PM_REPORT_PERIOD", "PM_REPORT_PERIOD"); v != "" {
		cfg.PM.ReportPeriod = v
	}
	if v, ok := envBool("EMS_CONTROL_ENABLED", "CONTROL_ENABLED"); ok {
		cfg.Control.Enabled = v
	}
	if v := envString("EMS_CONTROL_ADDR", "CONTROL_ADDR"); v != "" {
		cfg.Control.Addr = v
	}
	if v := envString("EMS_CONTROL_DOCKER_SOCKET", "CONTROL_DOCKER_SOCKET"); v != "" {
		cfg.Control.Restart.DockerSocket = v
	}
	if v := envString("EMS_CONTROL_RESTART_TIMEOUT", "CONTROL_RESTART_TIMEOUT"); v != "" {
		cfg.Control.Restart.Timeout = v
	}
}

func envString(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func envInt(keys ...string) (int, bool) {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			var out int
			_, err := fmt.Sscanf(v, "%d", &out)
			if err == nil {
				return out, true
			}
		}
	}
	return 0, false
}

func envBool(keys ...string) (bool, bool) {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			switch v {
			case "1", "true", "TRUE", "yes", "YES", "y", "Y":
				return true, true
			case "0", "false", "FALSE", "no", "NO", "n", "N":
				return false, true
			}
		}
	}
	return false, false
}
