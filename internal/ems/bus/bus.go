package bus

import "context"

type Message interface{}

type Bus struct {
	ch chan Message
}

func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 100
	}
	return &Bus{ch: make(chan Message, buffer)}
}

func (b *Bus) Publish(msg Message) {
	select {
	case b.ch <- msg:
	default:
		// Drop if the buffer is full to avoid blocking critical paths.
	}
}

func (b *Bus) Subscribe(ctx context.Context) <-chan Message {
	out := make(chan Message)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-b.ch:
				out <- msg
			}
		}
	}()
	return out
}
