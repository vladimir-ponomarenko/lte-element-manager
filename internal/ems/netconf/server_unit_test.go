package netconf

import "testing"

func TestExtractMessageID(t *testing.T) {
	if got := extractMessageID(`<rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="14"></rpc>`); got != "14" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := extractMessageID(`<rpc/>`); got != "0" {
		t.Fatalf("unexpected: %s", got)
	}
	if got := extractMessageID(`message-id="`); got != "0" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestRPCReplies_DefaultMessageID(t *testing.T) {
	if got := rpcReply("", "<ok/>"); got == "" {
		t.Fatalf("expected reply")
	}
	if got := rpcErrorReply("", "x"); got == "" {
		t.Fatalf("expected error reply")
	}
}
