package bus

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

type Message interface{}

type Bus struct {
	mu     sync.RWMutex
	buffer int
	subs   map[chan Message]struct{}
	log    zerolog.Logger
	drops  atomic.Uint64
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

func NewWithLogger(buffer int, log zerolog.Logger) *Bus {
	b := New(buffer)
	b.log = log
	return b
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
			n := b.drops.Add(1)
			if n == 1 || n%100 == 0 {
				b.log.Warn().Uint64("drop_count", n).Int("subscribers", len(b.subs)).Msg("bus saturated, dropping message")
			}
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
