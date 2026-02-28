package ingest

import (
	"context"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

type Service struct {
	cfg           config.Config
	store         model.Store
	indexingState *appstate.IndexingState
}

func NewService(cfg config.Config, store model.Store) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
	}
}

func (s *Service) SetIndexingState(state *appstate.IndexingState) {
	s.indexingState = state
}

func (s *Service) Run(ctx context.Context) error {
	_ = ctx
	_ = s.cfg
	_ = s.store
	if s.indexingState != nil {
		s.indexingState.SetMode(appstate.ModeIncremental)
		s.indexingState.SetRunning(true)
		defer s.indexingState.SetRunning(false)
	}
	return model.ErrNotImplemented
}

func (s *Service) Reindex(ctx context.Context) error {
	_ = ctx
	_ = s.cfg
	_ = s.store
	if s.indexingState != nil {
		s.indexingState.SetMode(appstate.ModeFull)
		s.indexingState.SetRunning(true)
		defer s.indexingState.SetRunning(false)
	}
	return model.ErrNotImplemented
}
