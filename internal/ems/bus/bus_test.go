package bus

import (
	"context"
	"testing"
	"time"
)

func TestNew_DefaultBuffer(t *testing.T) {
	b := New(0)
	if b.ch == nil {
		t.Fatalf("nil channel")
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
