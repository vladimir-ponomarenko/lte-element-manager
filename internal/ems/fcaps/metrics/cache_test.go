package metrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
)

func TestCacheWritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "metrics.json")

	b := bus.New(10)
	store := NewStore()
	log := zerolog.Nop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Cache(ctx, b, store, snapshot, log)

	raw := `{"type":"enb_metrics","enb_serial":"enb-1","timestamp":1}`
	b.Publish(Event{Sample: domain.MetricSample{RawJSON: raw}})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(snapshot); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("snapshot not written")
		}
		time.Sleep(10 * time.Millisecond)
	}

	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(data) != raw {
		t.Fatalf("snapshot mismatch: %s", string(data))
	}
}
