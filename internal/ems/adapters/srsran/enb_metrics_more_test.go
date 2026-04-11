package srsran

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lte-element-manager/internal/ems/domain"
	emserrors "lte-element-manager/internal/errors"
)

type deadlineErrConn struct{}

func (deadlineErrConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, errors.New("read") }
func (deadlineErrConn) WriteTo([]byte, net.Addr) (int, error)  { return 0, nil }
func (deadlineErrConn) Close() error                           { return nil }
func (deadlineErrConn) LocalAddr() net.Addr                    { return &net.UnixAddr{Name: "x", Net: "unix"} }
func (deadlineErrConn) SetDeadline(time.Time) error            { return nil }
func (deadlineErrConn) SetReadDeadline(time.Time) error        { return errors.New("deadline") }
func (deadlineErrConn) SetWriteDeadline(time.Time) error       { return nil }

func TestENBMetricsReader_EmptySocketPath(t *testing.T) {
	r := &ENBMetricsReader{}
	err := r.Run(context.Background(), make(chan domain.MetricSample))
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeConfig {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
}

func TestENBMetricsReader_StopsOnContextCancelAfterTimeout(t *testing.T) {
	prev := udsReadDeadline
	udsReadDeadline = 10 * time.Millisecond
	defer func() { udsReadDeadline = prev }()

	dir := t.TempDir()
	sock := filepath.Join(dir, "metrics.uds")

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan domain.MetricSample, 1)
	r := &ENBMetricsReader{SocketPath: sock}
	errCh := make(chan error, 1)
	go func() { errCh <- r.Run(ctx, out) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Skipf("socket bind not permitted in this environment: %v", err)
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
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}

func TestENBMetricsReader_RemoveExistingSocketError(t *testing.T) {
	dir := t.TempDir()
	// Make os.Remove fail deterministically by creating a non-empty directory at SocketPath.
	sock := filepath.Join(dir, "metrics.uds")
	if err := os.MkdirAll(sock, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sock, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := &ENBMetricsReader{SocketPath: sock}
	err := r.Run(context.Background(), make(chan domain.MetricSample))
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeIO {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
}

func TestENBMetricsReader_SetReadDeadlineError(t *testing.T) {
	prev := listenPacket
	listenPacket = func(string, string) (net.PacketConn, error) { return deadlineErrConn{}, nil }
	defer func() { listenPacket = prev }()

	r := &ENBMetricsReader{SocketPath: filepath.Join(t.TempDir(), "metrics.uds")}
	err := r.Run(context.Background(), make(chan domain.MetricSample))
	if err == nil {
		t.Fatalf("expected error")
	}
	if emserrors.CodeOf(err) != emserrors.ErrCodeNetwork {
		t.Fatalf("unexpected code: %s", emserrors.CodeOf(err))
	}
}
