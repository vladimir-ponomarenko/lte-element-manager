package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTracker_EventsAndState(t *testing.T) {
	tr := New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := tr.Subscribe(ctx)

	tr.Up(ComponentNetconf)

	gotUp := false
	gotState := false

	deadline := time.After(2 * time.Second)
	for !(gotUp && gotState) {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for events (up=%v state=%v)", gotUp, gotState)
		case ev := <-sub:
			switch ev.Type {
			case EventComponentUp:
				if ev.Component != ComponentNetconf || !ev.Up {
					t.Fatalf("unexpected up event: %+v", ev)
				}
				if ev.State != StateHealthy {
					t.Fatalf("expected state healthy, got %s", ev.State)
				}
				gotUp = true
			case EventStateChange:
				if ev.PrevState != StateUnknown || ev.State != StateHealthy {
					t.Fatalf("unexpected state change: prev=%s state=%s", ev.PrevState, ev.State)
				}
				gotState = true
			}
		}
	}

	tr.Down(ComponentUDS, errors.New("socket gone"))
	if tr.State() != StateDegraded {
		t.Fatalf("expected degraded, got %s", tr.State())
	}

	tr.Down(ComponentNetconf, errors.New("netconf crashed"))
	if tr.State() != StateCritical {
		t.Fatalf("expected critical, got %s", tr.State())
	}
}

func TestTracker_SubscribeReplayAndErrChange(t *testing.T) {
	tr := New()
	tr.Down(ComponentUDS, errors.New("e1"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := tr.Subscribe(ctx)
	ev := <-ch
	if ev.Type != EventComponentDown || ev.Component != ComponentUDS {
		t.Fatalf("unexpected replay: %+v", ev)
	}

	tr.Down(ComponentUDS, errors.New("e2"))
	found := false
	deadline := time.After(2 * time.Second)
	for !found {
		select {
		case <-deadline:
			t.Fatalf("timeout")
		case ev = <-ch:
			if ev.Type == EventComponentDown && ev.Err != nil && ev.Err.Error() == "e2" {
				found = true
			}
		}
	}
}

func TestDerive_AllBranches(t *testing.T) {
	now := time.Now()
	_ = now

	m := map[Component]ComponentStatus{}
	if got := derive(m); got != StateUnknown {
		t.Fatalf("expected unknown, got %s", got)
	}

	m = map[Component]ComponentStatus{ComponentUDS: {Up: true}}
	if got := derive(m); got != StateHealthy {
		t.Fatalf("expected healthy, got %s", got)
	}

	m = map[Component]ComponentStatus{ComponentUDS: {Up: false}}
	if got := derive(m); got != StateDegraded {
		t.Fatalf("expected degraded, got %s", got)
	}

	m = map[Component]ComponentStatus{ComponentNetconf: {Up: false}}
	if got := derive(m); got != StateDegraded {
		t.Fatalf("expected degraded, got %s", got)
	}

	m = map[Component]ComponentStatus{
		ComponentUDS:     {Up: false},
		ComponentNetconf: {Up: false},
	}
	if got := derive(m); got != StateCritical {
		t.Fatalf("expected critical, got %s", got)
	}
}

func TestTracker_NoEventsOnNoChange(t *testing.T) {
	tr := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := tr.Subscribe(ctx)
	tr.Up(ComponentUDS)

	// Drain expected initial events.
	deadline := time.After(2 * time.Second)
	need := 2 // component_up + state_change
	for need > 0 {
		select {
		case <-deadline:
			t.Fatalf("timeout draining initial events")
		case <-ch:
			need--
		}
	}

	tr.Up(ComponentUDS)

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTracker_BroadcastDropPath(t *testing.T) {
	tr := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe but never drain the channel to force drops.
	_ = tr.Subscribe(ctx)

	for i := 0; i < 100; i++ {
		tr.Up(ComponentUDS)
		tr.Down(ComponentUDS, errors.New("x"))
	}
}
