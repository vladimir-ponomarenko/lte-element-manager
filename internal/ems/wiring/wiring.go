package wiring

import (
	"context"
	"path/filepath"
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
	"lte-element-manager/internal/ems/fcaps/tca"
	"lte-element-manager/internal/ems/health"
	"lte-element-manager/internal/ems/logging"
	mediationSRSRAN "lte-element-manager/internal/ems/mediation/srsran"
	"lte-element-manager/internal/ems/netconf"
	"lte-element-manager/internal/ems/netconfcm"
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

	b := bus.NewWithLogger(c.cfg.Bus.Buffer, logging.WithComponent(c.log, c.cfg.Log, "bus"))
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

	snapshotPath := c.cfg.Netconf.SnapshotPath
	if snapshotPath == "" {
		snapshotPath = c.cfg.Metrics.SnapshotPath
	}

	if domain.ElementType(c.cfg.Element.Type) == domain.ElementEPC {
		runner := service.NewRunner(c.log)
		h := health.New()
		reader := services.NewMetricsReader(agent, rawIn, logAdapter, h)
		reader.LogUDS = true
		runner.Add(reader)
		runner.Add(services.NewRawMetricsSink(rawIn, snapshotPath, logMetrics))
		return runner, nil
	}

	aalPath := ""
	if snapshotPath != "" {
		aalPath = filepath.Join(filepath.Dir(snapshotPath), "aal_state.json")
	}
	alarmStore, err := alarms.NewPersistentStore(aalPath)
	if err != nil {
		return nil, emserrors.Wrap(err, emserrors.ErrCodeConfig, "failed to load active alarm list",
			emserrors.WithOp("wiring"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	alarmStore.OnPersistError(func(err error) {
		logFaults.Error().Err(err).Str("path", aalPath).Msg("active alarm list persistence failed")
	})
	alarmMgr := alarms.NewManager(alarmStore)
	notificationQueue := alarms.NewNotificationQueue(alarms.DefaultNotificationQueueCapacity)
	alarmMgr.OnEvent = func(evt alarms.Event) {
		if err := notificationQueue.AppendAlarmEvent(evt); err != nil {
			logFaults.Error().Err(err).Str("alarm_id", evt.Alarm.AlarmID).Msg("failed to enqueue alarm notification")
		}
	}
	runner.Add(services.NewFaultService(b, h, alarmMgr, logFaults))
	tcaCfg, err := buildTCAConfig(c.cfg.FM.TCA)
	if err != nil {
		return nil, err
	}
	if tcaCfg.Enabled {
		runner.Add(services.NewTCAService(b, alarmMgr, tcaCfg, logFaults))
	}

	reg, err := nrm.New(nrm.Config{
		SubNetwork:     c.cfg.NRM.SubNetwork,
		ManagedElement: c.cfg.NRM.ManagedElement,
		ENBFunctionID:  c.cfg.NRM.ENBFunctionID,
	})
	if err != nil {
		return nil, err
	}
	alarmLogPath := filepath.Join(filepath.Dir(c.cfg.Element.SocketPath), c.cfg.NRM.ManagedElement+"_alarms.log")
	alarmMOI := "SubNetwork=" + c.cfg.NRM.SubNetwork + "/ManagedElement=" + c.cfg.NRM.ManagedElement + "/ENBFunction=" + c.cfg.NRM.ENBFunctionID
	runner.Add(services.NewAlarmLogReader(alarmLogPath, alarmMOI, b, alarmMgr, logFaults))

	reader := services.NewMetricsReader(agent, rawIn, logAdapter, h)
	reader.LogUDS = c.cfg.Metrics.LogUDS
	runner.Add(reader)

	runner.Add(services.NewRawFanout(rawIn, rawForMapping, rawForSnapshot, logAdapter))
	pmStore := pm.NewStore()
	runner.Add(services.NewNetconfSnapshot(
		rawForSnapshot,
		b,
		rawStore,
		snapshotPath,
		netconf.SnapshotConfig{
			SubNetwork:     c.cfg.NRM.SubNetwork,
			ManagedElement: c.cfg.NRM.ManagedElement,
			ENBFunctionID:  c.cfg.NRM.ENBFunctionID,
		},
		reg,
		pmStore,
		alarmStore,
		logMetrics,
	))

	mapper := &mediationSRSRAN.Mapper{SourceID: c.cfg.NRM.ManagedElement}
	runner.Add(services.NewMetricsConsumer(rawForMapping, b, mapper, logMetrics))
	runner.Add(services.NewTelemetryCache(b, telemetryStore, logMetrics))

	pmEnabled := c.cfg.PM.Enabled || tcaCfg.Enabled
	if pmEnabled {
		pmCfg, err := pm.ParseConfig(c.cfg.PM.GranularityPeriod, c.cfg.PM.ReportPeriod)
		if err != nil {
			return nil, err
		}
		if !c.cfg.PM.Enabled && tcaCfg.Enabled {
			logPM.Warn().Msg("pm engine enabled automatically because fm.tca.enabled=true")
		}
		engine := pm.NewEngine(b, reg, pmStore, pmCfg, logPM)
		runner.Add(services.NewPMEngine(engine, logPM))
	}

	var (
		cfgStore      *configuration.Store
		sup           worker.LifecycleSupervisor
		netconfMgr    *netconfcm.Manager
		runningPath   string
		candidatePath string
		controlURL    string
		controlUnix   string
	)

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

			if cfgStore == nil &&
				strings.TrimSpace(t.ENBConfigPath) != "" &&
				strings.TrimSpace(t.RRConfigPath) != "" &&
				strings.TrimSpace(t.SIBConfigPath) != "" &&
				strings.TrimSpace(t.RBConfigPath) != "" {
				store, storeErr := configuration.NewStore(
					strings.TrimSpace(t.ENBConfigPath),
					strings.TrimSpace(t.RRConfigPath),
					strings.TrimSpace(t.SIBConfigPath),
					strings.TrimSpace(t.RBConfigPath),
				)
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
		dockerSup := worker.NewDockerLifecycleSupervisor(c.cfg.Control.Restart.DockerSocket, timeout, logControl)
		dockerSup.SetPlans(plans)
		sup = dockerSup

		if strings.TrimSpace(c.cfg.Control.Addr) != "" {
			controlURL = controlLocalURL(c.cfg.Control.Addr)
			if strings.HasPrefix(strings.TrimSpace(c.cfg.Control.Addr), "unix:") {
				controlUnix = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.cfg.Control.Addr), "unix:"))
			}
		}
		if cfgStore != nil {
			datastoreDir := strings.TrimSpace(c.cfg.Netconf.DatastoreDir)
			if datastoreDir == "" && snapshotPath != "" {
				datastoreDir = filepath.Join(filepath.Dir(snapshotPath), "datastores")
			}
			if datastoreDir != "" {
				runningPath = filepath.Join(datastoreDir, "running.json")
				candidatePath = filepath.Join(datastoreDir, "candidate.json")
				manager, mgrErr := netconfcm.NewManager(
					c.cfg.Netconf.YangDir,
					netconfcm.IDs{
						SubNetwork:     c.cfg.NRM.SubNetwork,
						ManagedElement: c.cfg.NRM.ManagedElement,
						ENBFunctionID:  c.cfg.NRM.ENBFunctionID,
					},
					netconfcm.ArtifactPaths{Running: runningPath, Candidate: candidatePath},
					cfgStore,
					sup,
					func(serial string) (string, bool) {
						serial = strings.TrimSpace(serial)
						if serial == "" {
							return "", false
						}
						if target, ok := targets[serial]; ok && strings.TrimSpace(target) != "" {
							return target, true
						}
						if len(targets) == 1 {
							for _, target := range targets {
								if strings.TrimSpace(target) != "" {
									return target, true
								}
							}
						}
						return "", false
					},
					logControl,
				)
				if mgrErr != nil {
					return nil, emserrors.Wrap(mgrErr, emserrors.ErrCodeConfig, "failed to initialize netconf CM manager",
						emserrors.WithOp("wiring"),
						emserrors.WithSeverity(emserrors.SeverityCritical),
					)
				}
				netconfMgr = manager
				runner.Add(services.NewNetconfSessionGC(netconfMgr, logControl))
			}
		}

		controlSvc := services.NewConfigControl(c.cfg.Control.Addr, targets, sup, cfgStore, netconfMgr, notificationQueue, logControl)
		controlSvc.Bus = b
		controlSvc.TCATestInject = c.cfg.FM.TCA.TestInjectionEnabled
		runner.Add(controlSvc)
		logControl.Info().
			Str("addr", c.cfg.Control.Addr).
			Int("targets", len(targets)).
			Msg("control api enabled")
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
			server := &netconf.ProcessServer{
				Binary:            "/app/netconf-server",
				Addr:              c.cfg.Netconf.Addr,
				YangDir:           c.cfg.Netconf.YangDir,
				SnapshotPath:      c.cfg.Netconf.SnapshotPath,
				RunningPath:       runningPath,
				CandidatePath:     candidatePath,
				ControlURL:        controlURL,
				ControlUnixSocket: controlUnix,
				HostKey:           c.cfg.Netconf.SSH.HostKey,
				AuthorizedKey:     c.cfg.Netconf.SSH.AuthorizedKey,
				Username:          c.cfg.Netconf.SSH.Username,
				Log:               logNetconf,
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

	return runner, nil
}

func buildTCAConfig(in config.TCAConfig) (tca.Config, error) {
	if !in.Enabled {
		return tca.Config{}, nil
	}
	rules := map[string]config.TCARuleConfig{
		tca.RuleS1InterfaceDown:        in.Rules.S1InterfaceDown,
		tca.RuleNASSignalingLoss:       in.Rules.NASSignalingLoss,
		tca.RuleNASSecurityMismatch:    in.Rules.NASSecurityMismatch,
		tca.RuleNASParsingFailure:      in.Rules.NASParsingFailure,
		tca.RuleRRCProtocolError:       in.Rules.RRCProtocolError,
		tca.RuleRRCConnectionRejection: in.Rules.RRCConnectionRejection,
		tca.RuleCoreServiceReject:      in.Rules.CoreServiceReject,
		tca.RulePagingCapacityExceeded: in.Rules.PagingCapacityExceeded,
		tca.RuleRLCMaxRetransmissions:  in.Rules.RLCMaxRetransmissions,
		tca.RuleLowThroughput:          in.Rules.LowThroughput,
		tca.RuleBadSignalCondition:     firstConfiguredRule(in.Rules.BadSignalCondition, in.Rules.LowULSINR),
		tca.RuleHighBLER:               in.Rules.HighBLER,
		tca.RuleRLFStorm:               in.Rules.RadioLinkFailureStorm,
		tca.RuleRFInterference:         in.Rules.RFInterferenceDetected,
		tca.RuleUEInactivityCleanup:    in.Rules.UEInactivityCleanup,
		tca.RuleBearerCongestion:       in.Rules.BearerCongestion,
		tca.RulePowerHeadroomCritical:  in.Rules.PowerHeadroomCritical,
	}
	out := tca.Config{Enabled: true, Rules: make(map[string]tca.RuleConfig, len(rules))}
	for name, raw := range rules {
		if !isTCARuleConfigured(raw) {
			continue
		}
		rule, err := buildTCARule(raw)
		if err != nil {
			return tca.Config{}, err
		}
		out.Rules[name] = rule
	}
	return tca.NormalizeConfig(out), nil
}

func firstConfiguredRule(primary, fallback config.TCARuleConfig) config.TCARuleConfig {
	if isTCARuleConfigured(primary) {
		return primary
	}
	return fallback
}

func isTCARuleConfigured(rule config.TCARuleConfig) bool {
	return rule.Enabled || rule.RaiseThreshold != 0 || rule.ClearThreshold != 0 ||
		rule.RaiseDuration != "" || rule.ClearDuration != ""
}

func buildTCARule(in config.TCARuleConfig) (tca.RuleConfig, error) {
	raiseDuration, err := parseTCADuration(in.RaiseDuration)
	if err != nil {
		return tca.RuleConfig{}, err
	}
	clearDuration, err := parseTCADuration(in.ClearDuration)
	if err != nil {
		return tca.RuleConfig{}, err
	}
	return tca.RuleConfig{
		Enabled:        in.Enabled,
		RaiseThreshold: in.RaiseThreshold,
		ClearThreshold: in.ClearThreshold,
		RaiseDuration:  raiseDuration,
		ClearDuration:  clearDuration,
	}, nil
}

func parseTCADuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, emserrors.Wrap(err, emserrors.ErrCodeConfig, "invalid fm.tca rule duration",
			emserrors.WithOp("wiring"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	return d, nil
}
