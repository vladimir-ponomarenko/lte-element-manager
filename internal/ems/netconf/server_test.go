package netconf

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/metrics"
)

func TestReadNetconf(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	r := bufio.NewReader(c2)
	go func() {
		_, _ = c1.Write([]byte("<x/>" + endMarker))
	}()
	out, err := readNetconf(r)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != "<x/>" {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestServer_HandleConn_Get(t *testing.T) {
	store := metrics.NewStore()
	store.Update(domain.MetricSample{RawJSON: `{"type":"enb_metrics","enb_serial":"x","timestamp":"1"}`})

	s := &Server{Store: store, Log: zerolog.Nop()}

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.handleConn(ctx, c2)

	rd := bufio.NewReader(c1)
	hello, err := readNetconf(rd)
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if !strings.Contains(hello, "<hello") {
		t.Fatalf("unexpected hello: %q", hello)
	}

	req := `<?xml version="1.0" encoding="UTF-8"?><rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><get/></rpc>` + endMarker
	if _, err := c1.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := readNetconf(rd)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(resp, "rpc-reply") {
		t.Fatalf("unexpected resp: %q", resp)
	}
}

func TestServer_HandleConn_Ok(t *testing.T) {
	s := &Server{Store: metrics.NewStore(), Log: zerolog.Nop()}

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.handleConn(ctx, c2)
	rd := bufio.NewReader(c1)
	_, _ = readNetconf(rd)

	req := `<?xml version="1.0" encoding="UTF-8"?><rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><close-session/></rpc>` + endMarker
	_, _ = c1.Write([]byte(req))
	_ = c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := readNetconf(rd)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(resp, "<ok/>") {
		t.Fatalf("unexpected: %q", resp)
	}
}

func TestExtractMessageID_Default(t *testing.T) {
	if extractMessageID("<rpc/>") != "0" {
		t.Fatalf("expected 0")
	}
}

func TestRPCReplies(t *testing.T) {
	if !strings.Contains(rpcReply("", "<ok/>"), "message-id=\"0\"") {
		t.Fatalf("expected default id")
	}
	if !strings.Contains(rpcErrorReply("", "x"), "rpc-error") {
		t.Fatalf("expected rpc-error")
	}
}

func TestServer_Run_AddrEmpty(t *testing.T) {
	s := &Server{Addr: "", Store: metrics.NewStore(), Log: zerolog.Nop()}
	if err := s.Run(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestServer_Run_Cancel(t *testing.T) {
	s := &Server{Addr: "127.0.0.1:0", Store: metrics.NewStore(), Log: zerolog.Nop()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
