package gateway

import "context"

// Gateway is the northbound boundary (NETCONF/YANG in the target design).
type Gateway struct{}

func (g *Gateway) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
