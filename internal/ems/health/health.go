package health

import (
	"context"
	"sync"
	"time"
)

type State string

const (
	StateUnknown  State = "unknown"
	StateHealthy  State = "healthy"
	StateDegraded State = "degraded"
	StateCritical State = "critical"
)

type Component string

const (
	ComponentUDS     Component = "uds"
	ComponentNetconf Component = "netconf"
)

type ComponentStatus struct {
	Up      bool
	At      time.Time
	LastErr error
}

type EventType string

const (
	EventComponentUp   EventType = "component_up"
	EventComponentDown EventType = "component_down"
	EventStateChange   EventType = "state_change"
)

// Event is a health signal emitted by Tracker.
type Event struct {
	Type      EventType
	At        time.Time
	Component Component
	Up        bool
	Err       error

	State     State
	PrevState State
}

// Tracker aggregates component status into an overall Health State.
type Tracker struct {
	mu         sync.Mutex
	components map[Component]ComponentStatus
	state      State
	subs       map[chan Event]struct{}
}

func New() *Tracker {
	return &Tracker{
		components: map[Component]ComponentStatus{},
		state:      StateUnknown,
		subs:       map[chan Event]struct{}{},
	}
}

// Subscribe returns a stream of health events until ctx is canceled.
// The returned channel is closed after unsubscription.
func (t *Tracker) Subscribe(ctx context.Context) <-chan Event {
	t.mu.Lock()
	ch := make(chan Event, len(t.components)+16)
	t.subs[ch] = struct{}{}
	state := t.state
	snap := make(map[Component]ComponentStatus, len(t.components))
	for k, v := range t.components {
		snap[k] = v
	}
	t.mu.Unlock()

	// Replay current component status.
	for c, st := range snap {
		typ := EventComponentUp
		if !st.Up {
			typ = EventComponentDown
		}
		ch <- Event{
			Type:      typ,
			At:        st.At,
			Component: c,
			Up:        st.Up,
			Err:       st.LastErr,
			State:     state,
			PrevState: state,
		}
	}

	go func() {
		<-ctx.Done()
		t.mu.Lock()
		delete(t.subs, ch)
		close(ch)
		t.mu.Unlock()
	}()

	return ch
}

func (t *Tracker) Up(c Component) {
	t.set(c, ComponentStatus{Up: true, At: time.Now()})
}

func (t *Tracker) Down(c Component, err error) {
	t.set(c, ComponentStatus{Up: false, At: time.Now(), LastErr: err})
}

func (t *Tracker) State() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

func (t *Tracker) Component(c Component) (ComponentStatus, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.components[c]
	return s, ok
}

func (t *Tracker) set(c Component, s ComponentStatus) {
	t.mu.Lock()
	prev, hadPrev := t.components[c]
	t.components[c] = s
	prevState := t.state
	next := derive(t.components)
	stateChanged := next != prevState
	t.state = next

	subs := make([]chan Event, 0, len(t.subs))
	for ch := range t.subs {
		subs = append(subs, ch)
	}
	t.mu.Unlock()

	now := s.At
	var events []Event

	componentChanged := !hadPrev || prev.Up != s.Up || (prev.LastErr == nil) != (s.LastErr == nil)
	if !componentChanged && prev.LastErr != nil && s.LastErr != nil && prev.LastErr.Error() != s.LastErr.Error() {
		componentChanged = true
	}
	if componentChanged {
		typ := EventComponentUp
		if !s.Up {
			typ = EventComponentDown
		}
		events = append(events, Event{
			Type:      typ,
			At:        now,
			Component: c,
			Up:        s.Up,
			Err:       s.LastErr,
			State:     next,
			PrevState: prevState,
		})
	}

	if stateChanged {
		events = append(events, Event{
			Type:      EventStateChange,
			At:        now,
			Component: "",
			Up:        false,
			Err:       nil,
			State:     next,
			PrevState: prevState,
		})
	}

	t.broadcast(subs, events)
}

func (t *Tracker) broadcast(subs []chan Event, events []Event) {
	for _, ev := range events {
		for _, ch := range subs {
			select {
			case ch <- ev:
			default:
				// Drop to avoid blocking critical paths.
			}
		}
	}
}

func derive(m map[Component]ComponentStatus) State {
	uds, udsOK := m[ComponentUDS]
	nc, ncOK := m[ComponentNetconf]

	udsDown := udsOK && !uds.Up
	ncDown := ncOK && !nc.Up

	switch {
	case udsDown && ncDown:
		return StateCritical
	case udsDown || ncDown:
		return StateDegraded
	case udsOK || ncOK:
		return StateHealthy
	default:
		return StateUnknown
	}
}
