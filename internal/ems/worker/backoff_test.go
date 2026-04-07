package worker

import (
	"math/rand"
	"testing"
	"time"
)

func TestExponentialBackoff_DefaultsAndSequence(t *testing.T) {
	b := NewExponentialBackoff(0, 0, 0, -1)
	b.rnd = rand.New(rand.NewSource(0))

	d1 := b.Next(0)
	if d1 != time.Second {
		t.Fatalf("expected 1s, got %v", d1)
	}
	d2 := b.Next(0)
	if d2 != 2*time.Second {
		t.Fatalf("expected 2s, got %v", d2)
	}
	d3 := b.Next(0)
	if d3 != 4*time.Second {
		t.Fatalf("expected 4s, got %v", d3)
	}
}

func TestExponentialBackoff_MaxAndReset(t *testing.T) {
	b := NewExponentialBackoff(1*time.Second, 3*time.Second, 2*time.Second, 0)
	d1 := b.Next(0)
	_ = d1
	d2 := b.Next(0)
	_ = d2
	d3 := b.Next(0)
	if d3 != 3*time.Second {
		t.Fatalf("expected max 3s, got %v", d3)
	}

	d4 := b.Next(2 * time.Second)
	if d4 != 1*time.Second {
		t.Fatalf("expected reset to base, got %v", d4)
	}
}

func TestExponentialBackoff_Jitter(t *testing.T) {
	b := NewExponentialBackoff(1*time.Second, 10*time.Second, 10*time.Second, 0.5)
	b.rnd = rand.New(rand.NewSource(0))
	d := b.Next(0)
	if d < time.Second || d > 1500*time.Millisecond {
		t.Fatalf("unexpected jittered delay: %v", d)
	}
}
