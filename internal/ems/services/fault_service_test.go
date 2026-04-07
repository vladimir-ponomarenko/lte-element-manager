package services

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/fcaps/alarms"
	"lte-element-manager/internal/ems/health"
	emserrors "lte-element-manager/internal/errors"
)

func TestFaultService_PublishesAlarmsOnHealthTransitions(t *testing.T) {
	b := bus.New(10)
	h := health.New()
	m := alarms.NewManager(alarms.NewStore())

	svc := NewFaultService(b, h, m, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	sub := b.Subscribe(ctx)

	downErr := emserrors.New(emserrors.ErrCodeNetwork, "uds read failed",
		emserrors.WithOp("uds"),
		emserrors.WithSeverity(emserrors.SeverityMajor),
	)
	h.Down(health.ComponentUDS, downErr)

	var gotActive bool
	deadline := time.After(2 * time.Second)
	for !gotActive {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for active alarm event")
		case msg := <-sub:
			evt, ok := msg.(alarms.Event)
			if !ok {
				continue
			}
			if evt.Component != "uds" {
				continue
			}
			if evt.Status != alarms.StatusActive {
				t.Fatalf("expected active, got %s", evt.Status)
			}
			if evt.Health != string(health.StateDegraded) {
				t.Fatalf("expected degraded health, got %s", evt.Health)
			}
			if evt.Alarm.Code != "EMS_NETWORK" {
				t.Fatalf("unexpected alarm code: %s", evt.Alarm.Code)
			}
			gotActive = true
		}
	}

	h.Up(health.ComponentUDS)

	var gotCleared bool
	deadline = time.After(2 * time.Second)
	for !gotCleared {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for cleared alarm event")
		case msg := <-sub:
			evt, ok := msg.(alarms.Event)
			if !ok {
				continue
			}
			if evt.Component != "uds" {
				continue
			}
			if evt.Status != alarms.StatusCleared {
				continue
			}
			gotCleared = true
		}
	}

	cancel()
	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("fault service did not stop")
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestFaultService_BelowThreshold(t *testing.T) {
	b := bus.New(10)
	h := health.New()
	m := alarms.NewManager(alarms.NewStore())

	svc := NewFaultService(b, h, m, zerolog.Nop())
	svc.MinSeverity = emserrors.SeverityCritical

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = svc.Run(ctx) }()

	sub := b.Subscribe(ctx)
	h.Down(health.ComponentUDS, emserrors.New(emserrors.ErrCodeNetwork, "x", emserrors.WithSeverity(emserrors.SeverityMajor)))

	select {
	case <-sub:
		t.Fatalf("unexpected publish")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestFaultService_NilDeps(t *testing.T) {
	svc := NewFaultService(nil, nil, nil, zerolog.Nop())
	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
