package gateway

import (
	"context"
	"testing"
)

func TestGatewayStart(t *testing.T) {
	var g Gateway
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Start(ctx); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
