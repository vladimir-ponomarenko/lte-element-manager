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

func readMsg(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timeout")
		}
		chunk, err := r.ReadString('>')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		b.WriteString(chunk)
		if strings.Contains(b.String(), endMarker) {
			return strings.TrimSuffix(b.String(), endMarker)
		}
	}
}

func TestServer_Run_EmptyAddr(t *testing.T) {
	s := NewServer("", metrics.NewStore(), zerolog.Nop())
	if err := s.Run(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestServer_HandleConn_GetAndOK(t *testing.T) {
	store := metrics.NewStore()
	store.Update(domain.MetricSample{RawJSON: `{"type":"enb_metrics","timestamp":1,"enb_serial":"x"}`})

	s := NewServer("127.0.0.1:0", store, zerolog.Nop())

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.handleConn(ctx, c1)

	r := bufio.NewReader(c2)
	hello := readMsg(t, r)
	if !strings.Contains(hello, "<hello") {
		t.Fatalf("unexpected hello: %s", hello)
	}

	_, _ = c2.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get/></rpc>` + endMarker))
	reply := readMsg(t, r)
	if !strings.Contains(reply, `message-id="1"`) || !strings.Contains(reply, "<data") {
		t.Fatalf("unexpected reply: %s", reply)
	}

	_, _ = c2.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rpc message-id="2" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ping/></rpc>` + endMarker))
	reply = readMsg(t, r)
	if !strings.Contains(reply, `message-id="2"`) || !strings.Contains(reply, "<ok/>") {
		t.Fatalf("unexpected ok reply: %s", reply)
	}
}
