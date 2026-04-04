package worker

import (
	"math/rand"
	"time"
)

// Backoff provides the next delay after a failure.
type Backoff interface {
	Next(uptime time.Duration) time.Duration
}

// ExponentialBackoff implements 1s, 2s, 4s... up to Max, with optional jitter.
// If the worker has been running longer than ResetAfter, the backoff resets to Base.
type ExponentialBackoff struct {
	Base       time.Duration
	Max        time.Duration
	ResetAfter time.Duration
	Jitter     float64

	cur time.Duration
	rnd *rand.Rand
}

func NewExponentialBackoff(base, max, resetAfter time.Duration, jitter float64) *ExponentialBackoff {
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 30 * time.Second
	}
	if resetAfter <= 0 {
		resetAfter = 30 * time.Second
	}
	if jitter < 0 {
		jitter = 0
	}
	return &ExponentialBackoff{
		Base:       base,
		Max:        max,
		ResetAfter: resetAfter,
		Jitter:     jitter,
		rnd:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (b *ExponentialBackoff) Next(uptime time.Duration) time.Duration {
	if uptime >= b.ResetAfter {
		b.cur = 0
	}
	if b.cur == 0 {
		b.cur = b.Base
	} else {
		b.cur *= 2
		if b.cur > b.Max {
			b.cur = b.Max
		}
	}

	delay := b.cur
	if b.Jitter > 0 {
		extra := time.Duration(b.rnd.Float64() * b.Jitter * float64(delay))
		delay += extra
	}
	return delay
}
