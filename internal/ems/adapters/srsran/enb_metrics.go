package srsran

import (
	"context"
	"net"
	"os"
	"strings"
	"time"

	"lte-element-manager/internal/ems/domain"
	emserrors "lte-element-manager/internal/errors"
)

type ENBMetricsReader struct {
	SocketPath string
}

var udsReadDeadline = time.Second
var listenPacket = net.ListenPacket

func (r *ENBMetricsReader) Run(ctx context.Context, out chan<- domain.MetricSample) error {
	if r.SocketPath == "" {
		return emserrors.New(emserrors.ErrCodeConfig, "socket path is empty",
			emserrors.WithOp("uds"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	if err := os.Remove(r.SocketPath); err != nil && !os.IsNotExist(err) {
		return emserrors.Wrap(err, emserrors.ErrCodeIO, "remove existing socket failed",
			emserrors.WithOp("uds"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}
	conn, err := listenPacket("unixgram", r.SocketPath)
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeNetwork, "listen on unixgram socket failed",
			emserrors.WithOp("uds"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	defer os.Remove(r.SocketPath)
	defer conn.Close()

	buf := make([]byte, 65535)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(udsReadDeadline)); err != nil {
			return emserrors.Wrap(err, emserrors.ErrCodeNetwork, "set socket read deadline failed",
				emserrors.WithOp("uds"),
				emserrors.WithSeverity(emserrors.SeverityMajor),
			)
		}

		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			continue
		}

		msg := strings.ReplaceAll(string(buf[:n]), "\n", " ")
		out <- domain.MetricSample{RawJSON: msg}
	}
}
