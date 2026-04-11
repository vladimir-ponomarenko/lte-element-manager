package pm

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain/canonical"
	"lte-element-manager/internal/ems/domain/nrm"
	"lte-element-manager/internal/ems/telemetry"
	emserrors "lte-element-manager/internal/errors"
)

type Engine struct {
	Bus      *bus.Bus
	Registry *nrm.Registry
	Store    *Store
	Log      zerolog.Logger

	cfg Config

	windowStart time.Time
	acc         map[nrm.DN]map[string]*metricAcc
}

func NewEngine(b *bus.Bus, reg *nrm.Registry, store *Store, cfg Config, log zerolog.Logger) *Engine {
	if cfg.Granularity <= 0 {
		cfg.Granularity = 10 * time.Second
	}
	if cfg.ReportEvery <= 0 {
		cfg.ReportEvery = cfg.Granularity
	}
	return &Engine{
		Bus:         b,
		Registry:    reg,
		Store:       store,
		Log:         log,
		cfg:         cfg,
		windowStart: time.Now(),
		acc:         make(map[nrm.DN]map[string]*metricAcc, 64),
	}
}

func (e *Engine) Run(ctx context.Context) error {
	if e.Bus == nil || e.Registry == nil || e.Store == nil {
		return nil
	}

	sub := e.Bus.Subscribe(ctx)
	ticker := time.NewTicker(e.cfg.Granularity)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			e.flush(time.Now())
		case msg, ok := <-sub:
			if !ok {
				return nil
			}
			switch m := msg.(type) {
			case telemetry.Event:
				e.ingest(m.Samples)
			case ConfigUpdate:
				e.applyConfig(m.Config)
			}
		}
	}
}

func (e *Engine) applyConfig(cfg Config) {
	if cfg.Granularity > 0 {
		e.cfg.Granularity = cfg.Granularity
	}
	if cfg.ReportEvery > 0 {
		e.cfg.ReportEvery = cfg.ReportEvery
	}
	e.Log.Info().
		Dur("granularity", e.cfg.Granularity).
		Dur("report_every", e.cfg.ReportEvery).
		Msg("pm config updated")
}

func (e *Engine) ingest(samples []canonical.Sample) {
	for _, s := range samples {
		dn, err := e.Registry.Resolve(s)
		if err != nil {
			e.Log.Debug().Err(err).Msg("pm resolve dn failed")
			continue
		}
		mm, ok := e.acc[dn]
		if !ok {
			mm = make(map[string]*metricAcc, len(s.Metrics))
			e.acc[dn] = mm
		}
		for name, m := range s.Metrics {
			a, ok := mm[name]
			if !ok {
				a = newMetricAcc(m)
				mm[name] = a
			}
			a.add(m.Value)
		}
	}
}

func (e *Engine) flush(now time.Time) {
	if now.Before(e.windowStart) {
		e.windowStart = now
	}
	begin := e.windowStart
	end := now

	report := Report{
		Begin:       begin,
		End:         end,
		Granularity: e.cfg.Granularity,
		ByDN:        make(map[nrm.DN]map[string]Value, len(e.acc)),
	}

	for dn, mm := range e.acc {
		out := make(map[string]Value, len(mm))
		for name, a := range mm {
			v, ok := a.value()
			if !ok {
				continue
			}
			out[name] = Value{Metric: a.metric, Value: v}
		}
		if len(out) > 0 {
			report.ByDN[dn] = out
		}
	}

	e.Store.Update(report)
	e.Bus.Publish(Event{Report: report})

	e.Log.Debug().
		Time("begin", begin).
		Time("end", end).
		Int("dn_count", len(report.ByDN)).
		Msg("pm report updated")

	e.windowStart = now
	e.acc = make(map[nrm.DN]map[string]*metricAcc, 64)
}

type metricAcc struct {
	metric canonical.Metric

	seen bool

	first float64
	last  float64

	sum   float64
	count int
}

func newMetricAcc(m canonical.Metric) *metricAcc {
	return &metricAcc{metric: m}
}

func (a *metricAcc) add(v float64) {
	if !a.seen {
		a.seen = true
		a.first = v
		a.last = v
		a.sum = v
		a.count = 1
		return
	}
	a.last = v
	a.sum += v
	a.count++
}

func (a *metricAcc) value() (float64, bool) {
	if !a.seen {
		return 0, false
	}
	switch a.metric.Type {
	case canonical.Gauge:
		if a.count == 0 {
			return 0, false
		}
		return a.sum / float64(a.count), true
	case canonical.Counter:
		delta := a.last - a.first
		if delta < 0 {
			// Counter reset within the window.
			return a.last, true
		}
		return delta, true
	default:
		return 0, false
	}
}

func ParseConfig(granularity, reportEvery string) (Config, error) {
	g, err := time.ParseDuration(granularity)
	if err != nil {
		return Config{}, emserrors.Wrap(err, emserrors.ErrCodeConfig, "pm granularity_period invalid",
			emserrors.WithOp("pm"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	r, err := time.ParseDuration(reportEvery)
	if err != nil {
		return Config{}, emserrors.Wrap(err, emserrors.ErrCodeConfig, "pm report_period invalid",
			emserrors.WithOp("pm"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	return Config{Granularity: g, ReportEvery: r}, nil
}
