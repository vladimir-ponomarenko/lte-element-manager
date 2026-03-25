package logging

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/config"
)

func New(cfg config.LogConfig) zerolog.Logger {
	level := parseLevel(cfg.Level)
	var logger zerolog.Logger

	if cfg.Format == "console" {
		writer := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		writer.NoColor = !cfg.Color
		if cfg.Color {
			writer.FormatLevel = func(i interface{}) string {
				level := strings.ToLower(fmt.Sprint(i))
				color, short := levelStyle(level)
				return color + short + colorReset
			}
		}
		logger = zerolog.New(writer)
	} else {
		logger = zerolog.New(os.Stdout)
	}

	if cfg.Timestamp {
		logger = logger.With().Timestamp().Logger()
	}

	return logger.Level(level)
}

func WithComponent(base zerolog.Logger, cfg config.LogConfig, component string) zerolog.Logger {
	logger := base.With().Str("component", component).Logger()
	if level, ok := cfg.Components[component]; ok && level != "" {
		if parsed, err := zerolog.ParseLevel(level); err == nil {
			return logger.Level(parsed)
		}
	}
	return logger
}

func parseLevel(level string) zerolog.Level {
	if level == "" {
		return zerolog.InfoLevel
	}
	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		return zerolog.InfoLevel
	}
	return parsed
}

const (
	colorRed     = "\x1b[31m"
	colorYellow  = "\x1b[33m"
	colorWhite   = "\x1b[37m"
	colorCyan    = "\x1b[36m"
	colorMagenta = "\x1b[35m"
	colorReset   = "\x1b[0m"
)

func levelStyle(level string) (string, string) {
	switch level {
	case "error", "fatal", "panic":
		return colorRed, "ERR"
	case "warn":
		return colorYellow, "WRN"
	case "info":
		return colorWhite, "INF"
	case "debug":
		return colorCyan, "DBG"
	case "trace":
		return colorMagenta, "TRC"
	default:
		return colorWhite, strings.ToUpper(level)
	}
}
