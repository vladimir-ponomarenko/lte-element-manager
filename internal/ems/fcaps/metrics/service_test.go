package metrics

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
)

func TestConsume_ParserBranchesAndChannelClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan domain.MetricSample, 2)
	b := bus.New(10)

	var warned bytes.Buffer
	log := zerolog.New(&warned).Level(zerolog.WarnLevel)

	go Consume(ctx, in, b, func([]byte) (any, error) { return nil, errors.New("bad") }, log)

	in <- domain.MetricSample{RawJSON: `{"x":1}`}
	close(in)

	sub := b.Subscribe(ctx)
	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for publish")
	case msg := <-sub:
		if _, ok := msg.(Event); !ok {
			t.Fatalf("unexpected msg type")
		}
	}

	if warned.String() == "" {
		t.Fatalf("expected warn log")
	}
}

func TestConsume_ParserSuccessSetsParsed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan domain.MetricSample, 1)
	b := bus.New(10)
	go Consume(ctx, in, b, func([]byte) (any, error) { return "ok", nil }, zerolog.Nop())

	in <- domain.MetricSample{RawJSON: `{"x":1}`}

	sub := b.Subscribe(ctx)
	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	case msg := <-sub:
		evt := msg.(Event)
		if evt.Sample.Parsed != "ok" {
			t.Fatalf("expected parsed value")
		}
	}
}

func TestLog_IgnoresUnknownMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := bus.New(10)
	var buf bytes.Buffer
	log := zerolog.New(&buf).Level(zerolog.InfoLevel)

	done := make(chan struct{})
	go func() {
		Log(ctx, b, log)
		close(done)
	}()

	b.Publish("not-an-event")
	b.Publish(Event{Sample: domain.MetricSample{RawJSON: `{"x":1}`}})

	deadline := time.Now().Add(2 * time.Second)
	for buf.String() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if buf.String() == "" {
		t.Fatalf("expected log output")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}
