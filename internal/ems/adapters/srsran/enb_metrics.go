package srsran

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"lte-element-manager/internal/ems/domain"
)

type ENBMetricsReader struct {
	SocketPath string
}

func (r *ENBMetricsReader) Run(ctx context.Context, out chan<- domain.MetricSample) error {
	if r.SocketPath == "" {
		return fmt.Errorf("socket path is empty")
	}
	if err := os.Remove(r.SocketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	conn, err := net.ListenPacket("unixgram", r.SocketPath)
	if err != nil {
		return err
	}
	defer os.Remove(r.SocketPath)
	defer conn.Close()

	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				continue
			}
			msg := strings.ReplaceAll(string(buf[:n]), "\n", " ")
			out <- domain.MetricSample{RawJSON: msg}
		}
	}
}
