package worker

import "context"

// Worker is a runnable unit supervised by the agent.
type Worker interface {
	Name() string
	Run(ctx context.Context) error
}
