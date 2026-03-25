package wiring

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/adapters/srsran"
	"lte-element-manager/internal/ems/app"
	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/config"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/metrics"
	"lte-element-manager/internal/ems/logging"
	"lte-element-manager/internal/ems/service"
	"lte-element-manager/internal/ems/services"
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
	_ = ctx

	logMetrics := logging.WithComponent(c.log, c.cfg.Log, "metrics")
	logAdapter := logging.WithComponent(c.log, c.cfg.Log, "adapter")

	b := bus.New(c.cfg.Bus.Buffer)
	metricsOut := make(chan domain.MetricSample, 200)

	metricsSource, err := srsran.NewMetricsSource(domain.ElementType(c.cfg.Element.Type), c.cfg.Element.SocketPath)
	if err != nil {
		return nil, err
	}
	agent := app.New(metricsSource)
	parser := metrics.ParserFor(domain.ElementType(c.cfg.Element.Type))

	runner := service.NewRunner(c.log)
	runner.Add(services.NewMetricsReader(agent, metricsOut, logAdapter))
	runner.Add(services.NewMetricsConsumer(metricsOut, b, parser, logMetrics))
	runner.Add(services.NewMetricsLogger(b, logMetrics))

	return runner, nil
}
