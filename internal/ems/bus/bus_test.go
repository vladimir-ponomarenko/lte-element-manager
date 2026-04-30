package bus

import (
	"context"
	"testing"
	"time"
)

func TestNew_DefaultBuffer(t *testing.T) {
	b := New(0)
	if b.buffer == 0 || b.subs == nil {
		t.Fatalf("bus defaults not initialized")
	}
}

func TestPublish_DropWhenFull(t *testing.T) {
	b := New(1)
	b.Publish(1)
	b.Publish(2)
}

func TestSubscribe_CancelCloses(t *testing.T) {
	b := New(10)
	ctx, cancel := context.WithCancel(context.Background())
	ch := b.Subscribe(ctx)

	b.Publish("x")
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for message")
	}

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}

func TestPublish_Fanout(t *testing.T) {
	b := New(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := b.Subscribe(ctx)
	ch2 := b.Subscribe(ctx)
	if got := b.SubscriberCount(); got != 2 {
		t.Fatalf("expected 2 subscribers, got %d", got)
	}
	b.Publish("x")

	for _, ch := range []<-chan Message{ch1, ch2} {
		select {
		case got := <-ch:
			if got != "x" {
				t.Fatalf("unexpected message: %#v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for fanout message")
		}
	}
}
