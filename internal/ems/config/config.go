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
