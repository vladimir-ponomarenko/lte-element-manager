package netconf

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/fcaps/metrics"
	emserrors "lte-element-manager/internal/errors"
)

const (
	endMarker = "]]>]]>"
)

var endMarkerBytes = []byte(endMarker)

type Server struct {
	Addr  string
	Store *metrics.Store
	Log   zerolog.Logger
}

func NewServer(addr string, store *metrics.Store, log zerolog.Logger) *Server {
	return &Server{Addr: addr, Store: store, Log: log}
}

func (s *Server) Name() string {
	return "netconf-tcp"
}

func (s *Server) Run(ctx context.Context) error {
	if s.Addr == "" {
		return emserrors.New(emserrors.ErrCodeConfig, "netconf addr is empty",
			emserrors.WithOp("netconf-tcp"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeNetwork, "netconf listen failed",
			emserrors.WithOp("netconf-tcp"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	defer ln.Close()

	s.Log.Info().Str("addr", s.Addr).Msg("netconf server started")

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))

	if err := writeNetconf(conn, helloMessage()); err != nil {
		s.Log.Error().Err(err).Msg("netconf hello write failed")
		return
	}

	reader := bufio.NewReader(conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		req, err := readNetconf(reader)
		if err != nil {
			return
		}

		msgID := extractMessageID(req)
		if strings.Contains(req, "<get") || strings.Contains(req, "<get-config") {
			payload, err := buildMetricsReply(s.Store)
			if err != nil {
				_ = writeNetconf(conn, rpcErrorReply(msgID, err.Error()))
				continue
			}
			_ = writeNetconf(conn, rpcReply(msgID, payload))
			continue
		}

		_ = writeNetconf(conn, rpcReply(msgID, "<ok/>"))
	}
}

func readNetconf(r *bufio.Reader) (string, error) {
	var buf bytes.Buffer
	for {
		chunk, err := r.ReadBytes('>')
		if err != nil {
			return "", err
		}
		buf.Write(chunk)
		b := buf.Bytes()
		if i := bytes.Index(b, endMarkerBytes); i >= 0 {
			return string(b[:i]), nil
		}
	}
}

func writeNetconf(w net.Conn, msg string) error {
	_, err := w.Write([]byte(msg + endMarker))
	return err
}

func helloMessage() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">` +
		`<capabilities>` +
		`<capability>urn:ietf:params:netconf:base:1.0</capability>` +
		`<capability>urn:ems:enb:metrics?module=ems-enb-metrics&amp;revision=2026-04-01</capability>` +
		`</capabilities>` +
		`</hello>`
}

func extractMessageID(req string) string {
	start := strings.Index(req, "message-id=\"")
	if start == -1 {
		return "0"
	}
	start += len("message-id=\"")
	end := strings.Index(req[start:], "\"")
	if end == -1 {
		return "0"
	}
	return req[start : start+end]
}

func rpcReply(messageID, payload string) string {
	if messageID == "" {
		messageID = "0"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="%s">`+
		`%s</rpc-reply>`, messageID, payload)
}

func rpcErrorReply(messageID, errMsg string) string {
	if messageID == "" {
		messageID = "0"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="%s">`+
		`<rpc-error>`+
		`<error-type>application</error-type>`+
		`<error-tag>operation-failed</error-tag>`+
		`<error-severity>error</error-severity>`+
		`<error-message>%s</error-message>`+
		`</rpc-error>`+
		`</rpc-reply>`, messageID, errMsg)
}
