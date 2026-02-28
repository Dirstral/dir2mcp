package ingest

import (
	"context"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/model"
)

type Service struct {
	cfg   config.Config
	store model.Store
}

func NewService(cfg config.Config, store model.Store) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
	}
}

func (s *Service) Run(ctx context.Context) error {
	_ = ctx
	_ = s.cfg
	_ = s.store
	return model.ErrNotImplemented
}

func (s *Service) Reindex(ctx context.Context) error {
	_ = ctx
	_ = s.cfg
	_ = s.store
	return model.ErrNotImplemented
}
