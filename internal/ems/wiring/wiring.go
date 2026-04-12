package wiring

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/adapters/srsran"
	"lte-element-manager/internal/ems/app"
	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/config"
	"lte-element-manager/internal/ems/configuration"
	"lte-element-manager/internal/ems/configuration/srsranconf"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/domain/nrm"
	"lte-element-manager/internal/ems/fcaps/alarms"
	"lte-element-manager/internal/ems/fcaps/metrics"
	"lte-element-manager/internal/ems/fcaps/pm"
	"lte-element-manager/internal/ems/health"
	"lte-element-manager/internal/ems/logging"
	mediationSRSRAN "lte-element-manager/internal/ems/mediation/srsran"
	"lte-element-manager/internal/ems/netconf"
	"lte-element-manager/internal/ems/service"
	"lte-element-manager/internal/ems/services"
	"lte-element-manager/internal/ems/telemetry"
	"lte-element-manager/internal/ems/worker"
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
	logPM := logging.WithComponent(c.log, c.cfg.Log, "pm")
	logControl := logging.WithComponent(c.log, c.cfg.Log, "control")

	b := bus.New(c.cfg.Bus.Buffer)
	rawIn := make(chan domain.MetricSample, 200)
	rawForMapping := make(chan domain.MetricSample, 200)
	rawForSnapshot := make(chan domain.MetricSample, 200)
	rawStore := metrics.NewStore()
	telemetryStore := telemetry.NewStore()

	metricsSource, err := srsran.NewMetricsSource(domain.ElementType(c.cfg.Element.Type), c.cfg.Element.SocketPath)
	if err != nil {
		return nil, err
	}
	agent := app.New(metricsSource)

	runner := service.NewRunner(c.log)
	h := health.New()

	alarmStore := alarms.NewStore()
	alarmMgr := alarms.NewManager(alarmStore)
	runner.Add(services.NewFaultService(b, h, alarmMgr, logFaults))

	reg, err := nrm.New(nrm.Config{
		SubNetwork:     c.cfg.NRM.SubNetwork,
		ManagedElement: c.cfg.NRM.ManagedElement,
		ENBFunctionID:  c.cfg.NRM.ENBFunctionID,
	})
	if err != nil {
		return nil, err
	}

	reader := services.NewMetricsReader(agent, rawIn, logAdapter, h)
	reader.LogUDS = c.cfg.Metrics.LogUDS
	runner.Add(reader)

	snapshotPath := c.cfg.Netconf.SnapshotPath
	if snapshotPath == "" {
		snapshotPath = c.cfg.Metrics.SnapshotPath
	}

	runner.Add(services.NewRawFanout(rawIn, rawForMapping, rawForSnapshot, logAdapter))
	pmStore := pm.NewStore()
	runner.Add(services.NewNetconfSnapshot(
		rawForSnapshot,
		rawStore,
		snapshotPath,
		netconf.SnapshotConfig{
			SubNetwork:     c.cfg.NRM.SubNetwork,
			ManagedElement: c.cfg.NRM.ManagedElement,
			ENBFunctionID:  c.cfg.NRM.ENBFunctionID,
		},
		reg,
		pmStore,
		logMetrics,
	))

	mapper := &mediationSRSRAN.Mapper{SourceID: c.cfg.NRM.ManagedElement}
	runner.Add(services.NewMetricsConsumer(rawForMapping, b, mapper, logMetrics))
	runner.Add(services.NewTelemetryCache(b, telemetryStore, logMetrics))

	if c.cfg.PM.Enabled {
		pmCfg, err := pm.ParseConfig(c.cfg.PM.GranularityPeriod, c.cfg.PM.ReportPeriod)
		if err != nil {
			return nil, err
		}
		engine := pm.NewEngine(b, reg, pmStore, pmCfg, logPM)
		runner.Add(services.NewPMEngine(engine, logPM))
	}

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
				controlURL := ""
				if c.cfg.Control.Enabled && strings.TrimSpace(c.cfg.Control.Addr) != "" {
					controlURL = controlLocalURL(c.cfg.Control.Addr)
				}
				server := &netconf.ProcessServer{
					Binary:        "/app/netconf-server",
					Addr:          c.cfg.Netconf.Addr,
					YangDir:       c.cfg.Netconf.YangDir,
				SnapshotPath:  c.cfg.Netconf.SnapshotPath,
				ControlURL:    controlURL,
				HostKey:       c.cfg.Netconf.SSH.HostKey,
				AuthorizedKey: c.cfg.Netconf.SSH.AuthorizedKey,
				Username:      c.cfg.Netconf.SSH.Username,
				Log:           logNetconf,
			}
			runner.Add(services.NewNetconfServer(server, logNetconf, h))
		} else {
			server := netconf.NewServer(c.cfg.Netconf.Addr, rawStore, logNetconf)
			runner.Add(services.NewNetconfServer(server, logNetconf, h))
		}
	} else {
		// Mark NETCONF as up when disabled so overall health reflects UDS connectivity.
		h.Up(health.ComponentNetconf)
	}

	if c.cfg.Control.Enabled {
		timeout, err := time.ParseDuration(c.cfg.Control.Restart.Timeout)
		if err != nil {
			return nil, emserrors.Wrap(err, emserrors.ErrCodeConfig, "invalid control.restart.timeout",
				emserrors.WithOp("wiring"),
				emserrors.WithSeverity(emserrors.SeverityCritical),
			)
		}
		targets := make(map[string]string, len(c.cfg.Control.Restart.Targets))
		plans := make(map[string]worker.RestartPlan, len(c.cfg.Control.Restart.Targets))
		var cfgStore *configuration.Store
		for _, t := range c.cfg.Control.Restart.Targets {
			container := strings.TrimSpace(t.Container)
			if container == "" {
				continue
			}
			serial := strings.TrimSpace(t.Serial)
			if serial == "" && strings.TrimSpace(t.ENBConfigPath) != "" {
				enbCfg, parseErr := srsranconf.ParseENB(strings.TrimSpace(t.ENBConfigPath))
				if parseErr != nil {
					return nil, emserrors.Wrap(parseErr, emserrors.ErrCodeConfig, "failed to read enb_config_path for restart target",
						emserrors.WithOp("wiring"),
						emserrors.WithSeverity(emserrors.SeverityCritical),
					)
				}
				serial = strings.TrimSpace(enbCfg.Serial)
			}
			if serial == "" {
				return nil, emserrors.New(emserrors.ErrCodeConfig, "restart target serial is empty (set serial or enb_config_path)",
					emserrors.WithOp("wiring"),
					emserrors.WithSeverity(emserrors.SeverityCritical),
				)
			}
			targets[serial] = container

			if cfgStore == nil && strings.TrimSpace(t.ENBConfigPath) != "" && strings.TrimSpace(t.RRConfigPath) != "" {
				store, storeErr := configuration.NewStore(strings.TrimSpace(t.ENBConfigPath), strings.TrimSpace(t.RRConfigPath))
				if storeErr != nil {
					return nil, emserrors.Wrap(storeErr, emserrors.ErrCodeConfig, "failed to initialize configuration store",
						emserrors.WithOp("wiring"),
						emserrors.WithSeverity(emserrors.SeverityCritical),
					)
				}
				cfgStore = store
			}
			delay := 5 * time.Second
			if strings.TrimSpace(t.DelayAfterStart) != "" {
				parsedDelay, parseErr := time.ParseDuration(t.DelayAfterStart)
				if parseErr != nil {
					return nil, emserrors.Wrap(parseErr, emserrors.ErrCodeConfig, "invalid control.restart.targets.delay_after_start",
						emserrors.WithOp("wiring"),
						emserrors.WithSeverity(emserrors.SeverityCritical),
					)
				}
				delay = parsedDelay
			}
			plans[container] = worker.RestartPlan{
				Primary:         container,
				Dependents:      append([]string(nil), t.Dependents...),
				DelayAfterStart: delay,
			}
		}
		if len(targets) == 0 {
			return nil, emserrors.New(emserrors.ErrCodeConfig, "control is enabled but no restart targets are configured",
				emserrors.WithOp("wiring"),
				emserrors.WithSeverity(emserrors.SeverityCritical),
			)
		}
		sup := worker.NewDockerLifecycleSupervisor(c.cfg.Control.Restart.DockerSocket, timeout, logControl)
		sup.SetPlans(plans)
		runner.Add(services.NewConfigControl(c.cfg.Control.Addr, targets, sup, cfgStore, logControl))
		logControl.Info().
			Str("addr", c.cfg.Control.Addr).
			Int("targets", len(targets)).
			Msg("control api enabled")
	}

	return runner, nil
}
