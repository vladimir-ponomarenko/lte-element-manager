package srsran

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"lte-element-manager/internal/ems/domain"
)

func TestENBMetricsReader(t *testing.T) {
	udsReadDeadline = 10 * time.Millisecond
	t.Cleanup(func() { udsReadDeadline = time.Second })

	dir := t.TempDir()
	sock := filepath.Join(dir, "metrics.uds")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan domain.MetricSample, 1)
	reader := &ENBMetricsReader{SocketPath: sock}
	errCh := make(chan error, 1)

	go func() {
		errCh <- reader.Run(ctx, out)
	}()

	// Wait for socket file to appear.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		select {
		case err := <-errCh:
			if err != nil {
				if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || os.IsPermission(err) {
					t.Skipf("socket bind not permitted in this environment: %v", err)
				}
				t.Fatalf("reader error: %v", err)
			}
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket not created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := []byte(`{"type":"enb_metrics","timestamp":1}`)
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case got := <-out:
		if got.RawJSON == "" {
			t.Fatalf("empty payload")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for sample")
	}
}

func TestENBMetricsReader_CancelNoData(t *testing.T) {
	udsReadDeadline = 10 * time.Millisecond
	t.Cleanup(func() { udsReadDeadline = time.Second })

	dir := t.TempDir()
	sock := filepath.Join(dir, "metrics2.uds")

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan domain.MetricSample, 1)
	reader := &ENBMetricsReader{SocketPath: sock}
	errCh := make(chan error, 1)

	go func() { errCh <- reader.Run(ctx, out) }()

	// Wait for socket.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		select {
		case err := <-errCh:
			if err != nil {
				if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || os.IsPermission(err) {
					t.Skipf("socket bind not permitted in this environment: %v", err)
				}
				t.Fatalf("reader error: %v", err)
			}
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket not created")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	}
}
