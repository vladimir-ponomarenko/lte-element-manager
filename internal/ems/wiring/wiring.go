package wiring

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/adapters/srsran"
	"lte-element-manager/internal/ems/app"
	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/config"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/alarms"
	"lte-element-manager/internal/ems/fcaps/metrics"
	"lte-element-manager/internal/ems/health"
	"lte-element-manager/internal/ems/logging"
	"lte-element-manager/internal/ems/netconf"
	"lte-element-manager/internal/ems/service"
	"lte-element-manager/internal/ems/services"
	emserrors "lte-element-manager/internal/errors"
)

// Container wires dependencies for the EMS agent.
type Container struct {
	cfg config.Config
	log zerolog.Logger
}

func New(cfg config.Config, log zerolog.Logger) *Container {
	return &Container{cfg: cfg, log: log}
}

// Build assembles services and returns a runner ready to execute.
func (c *Container) Build(ctx context.Context) (*service.Runner, error) {
	logMetrics := logging.WithComponent(c.log, c.cfg.Log, "metrics")
	logAdapter := logging.WithComponent(c.log, c.cfg.Log, "adapter")
	logNetconf := logging.WithComponent(c.log, c.cfg.Log, "netconf")
	logFaults := logging.WithComponent(c.log, c.cfg.Log, "faults")

	b := bus.New(c.cfg.Bus.Buffer)
	metricsOut := make(chan domain.MetricSample, 200)
	metricsStore := metrics.NewStore()

	metricsSource, err := srsran.NewMetricsSource(domain.ElementType(c.cfg.Element.Type), c.cfg.Element.SocketPath)
	if err != nil {
		return nil, err
	}
	agent := app.New(metricsSource)
	parser := metrics.ParserFor(domain.ElementType(c.cfg.Element.Type))

	runner := service.NewRunner(c.log)
	h := health.New()

	alarmStore := alarms.NewStore()
	alarmMgr := alarms.NewManager(alarmStore)
	runner.Add(services.NewFaultService(b, h, alarmMgr, logFaults))

	reader := services.NewMetricsReader(agent, metricsOut, logAdapter, h)
	reader.LogUDS = c.cfg.Metrics.LogUDS
	runner.Add(reader)
	runner.Add(services.NewMetricsConsumer(metricsOut, b, parser, logMetrics))
	snapshotPath := c.cfg.Netconf.SnapshotPath
	if snapshotPath == "" {
		snapshotPath = c.cfg.Metrics.SnapshotPath
	}
	runner.Add(services.NewMetricsCache(b, metricsStore, snapshotPath, logMetrics))

	if c.cfg.Netconf.Enabled {
		if c.cfg.Netconf.Transport == "ssh" {
			if c.cfg.Netconf.SSH.HostKey == "" || c.cfg.Netconf.SSH.AuthorizedKey == "" || c.cfg.Netconf.SSH.Username == "" {
				return nil, emserrors.New(emserrors.ErrCodeConfig, "netconf ssh config is incomplete",
					emserrors.WithOp("wiring"),
					emserrors.WithSeverity(emserrors.SeverityCritical),
				)
			}
			if c.cfg.Netconf.SnapshotPath == "" {
				return nil, emserrors.New(emserrors.ErrCodeConfig, "netconf snapshot_path is empty",
					emserrors.WithOp("wiring"),
					emserrors.WithSeverity(emserrors.SeverityCritical),
				)
			}
			server := &netconf.ProcessServer{
				Binary:        "/app/netconf-server",
				Addr:          c.cfg.Netconf.Addr,
				YangDir:       c.cfg.Netconf.YangDir,
				SnapshotPath:  c.cfg.Netconf.SnapshotPath,
				HostKey:       c.cfg.Netconf.SSH.HostKey,
				AuthorizedKey: c.cfg.Netconf.SSH.AuthorizedKey,
				Username:      c.cfg.Netconf.SSH.Username,
				Log:           logNetconf,
			}
			runner.Add(services.NewNetconfServer(server, logNetconf, h))
		} else {
			server := netconf.NewServer(c.cfg.Netconf.Addr, metricsStore, logNetconf)
			runner.Add(services.NewNetconfServer(server, logNetconf, h))
		}
	} else {
		// Mark NETCONF as up when disabled so overall health reflects UDS connectivity.
		h.Up(health.ComponentNetconf)
	}

	return runner, nil
}
