package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/fcaps/pm"
)

type PMEngine struct {
	Engine *pm.Engine
	Log    zerolog.Logger
}

func NewPMEngine(e *pm.Engine, log zerolog.Logger) *PMEngine {
	return &PMEngine{Engine: e, Log: log}
}

func (s *PMEngine) Name() string { return "pm" }

func (s *PMEngine) Run(ctx context.Context) error {
	if s.Engine == nil {
		return nil
	}
	return s.Engine.Run(ctx)
}
