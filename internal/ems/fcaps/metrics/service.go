package metrics

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
)

type Event struct {
	Sample domain.MetricSample
}

type ParseFunc func([]byte) (any, error)

func Consume(ctx context.Context, in <-chan domain.MetricSample, b *bus.Bus, parser ParseFunc, log zerolog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case s, ok := <-in:
			if !ok {
				return
			}
			if parser != nil && s.Parsed == nil {
				parsed, err := parser([]byte(s.RawJSON))
				if err != nil {
					log.Warn().Err(err).Msg("metrics parse failed")
				} else {
					s.Parsed = parsed
				}
			}
			b.Publish(Event{Sample: s})
		}
	}
}

func Log(ctx context.Context, b *bus.Bus, log zerolog.Logger) {
	sub := b.Subscribe(ctx)
	for msg := range sub {
		evt, ok := msg.(Event)
		if !ok {
			continue
		}
		log.Info().RawJSON("metrics", []byte(evt.Sample.RawJSON)).Msg("metrics")
	}
}
