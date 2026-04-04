package main

import (
	"context"
	"flag"
	"bufio"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/config"
	"lte-element-manager/internal/ems/logging"
	"lte-element-manager/internal/ems/netconf"
	"lte-element-manager/internal/ems/wiring"
)

func main() {
	selfCheck := flag.Bool("self-check", false, "print runtime wiring info and exit")
	configPath := flag.String("config", "", "path to ems-agent config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		bootLog := zerolog.New(os.Stderr).With().Timestamp().Logger()
		bootLog.Error().Err(err).Msg("config error")
		os.Exit(1)
	}

	log := logging.New(cfg.Log).With().Str("element", cfg.Element.Type).Logger()

	if *selfCheck {
		w := bufio.NewWriter(os.Stdout)
		_, _ = w.WriteString("netconf_enabled=" + strconv.FormatBool(netconf.Enabled()) + "\n")
		_, _ = w.WriteString("config_path=" + *configPath + "\n")
		_, _ = w.WriteString("element_type=" + cfg.Element.Type + "\n")
		_, _ = w.WriteString("socket_path=" + cfg.Element.SocketPath + "\n")
		_, _ = w.WriteString("bus_buffer=" + strconv.Itoa(cfg.Bus.Buffer) + "\n")
		_ = w.Flush()
		return
	}

	log.Info().
		Str("socket_path", cfg.Element.SocketPath).
		Str("log_format", cfg.Log.Format).
		Bool("log_color", cfg.Log.Color).
		Bool("netconf_enabled", cfg.Netconf.Enabled).
		Str("netconf_addr", cfg.Netconf.Addr).
		Msg("ems agent started")
	container := wiring.New(cfg, log)
	runner, err := container.Build(ctx)
	if err != nil {
		log.Error().Err(err).Msg("wiring failed")
		return
	}

	if err := runner.Run(ctx); err != nil {
		log.Error().Err(err).Msg("ems agent stopped with error")
		return
	}
	log.Info().Msg("ems agent stopped")
}
