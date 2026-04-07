package main

import (
	"bufio"
	"context"
	"flag"
	"io"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ems-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	selfCheck := fs.Bool("self-check", false, "print runtime wiring info and exit")
	configPath := fs.String("config", "", "path to ems-agent config file")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		bootLog := zerolog.New(stderr).With().Timestamp().Logger()
		bootLog.Error().Err(err).Msg("config error")
		return 1
	}

	log := logging.New(cfg.Log).With().Str("element", cfg.Element.Type).Logger()

	if *selfCheck {
		w := bufio.NewWriter(stdout)
		_, _ = w.WriteString("netconf_enabled=" + strconv.FormatBool(netconf.Enabled()) + "\n")
		_, _ = w.WriteString("config_path=" + *configPath + "\n")
		_, _ = w.WriteString("element_type=" + cfg.Element.Type + "\n")
		_, _ = w.WriteString("socket_path=" + cfg.Element.SocketPath + "\n")
		_, _ = w.WriteString("bus_buffer=" + strconv.Itoa(cfg.Bus.Buffer) + "\n")
		_ = w.Flush()
		return 0
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
		return 0
	}

	if err := runner.Run(ctx); err != nil {
		log.Error().Err(err).Msg("ems agent stopped with error")
		return 0
	}
	log.Info().Msg("ems agent stopped")
	return 0
}
