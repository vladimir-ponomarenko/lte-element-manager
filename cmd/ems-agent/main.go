package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Log).With().Str("element", cfg.Element.Type).Logger()

	if *selfCheck {
		fmt.Printf("netconf_enabled=%v\n", netconf.Enabled())
		fmt.Printf("config_path=%s\n", *configPath)
		fmt.Printf("element_type=%s\n", cfg.Element.Type)
		fmt.Printf("socket_path=%s\n", cfg.Element.SocketPath)
		fmt.Printf("bus_buffer=%d\n", cfg.Bus.Buffer)
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
