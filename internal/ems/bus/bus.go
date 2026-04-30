package bus

import (
	"context"
	"sync"
)

type Message interface{}

type Bus struct {
	mu     sync.RWMutex
	buffer int
	subs   map[chan Message]struct{}
}

func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 100
	}
	return &Bus{
		buffer: buffer,
		subs:   make(map[chan Message]struct{}),
	}
}

func (b *Bus) Publish(msg Message) {
	if b == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
			// Drop for slow subscribers so critical paths do not block.
		}
	}
}

func (b *Bus) Subscribe(ctx context.Context) <-chan Message {
	out := make(chan Message, b.buffer)
	b.mu.Lock()
	b.subs[out] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, out)
		close(out)
		b.mu.Unlock()
	}()
	return out
}

func (b *Bus) SubscriberCount() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
