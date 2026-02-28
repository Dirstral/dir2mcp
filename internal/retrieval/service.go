package retrieval

import (
	"context"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type Service struct {
	store model.Store
	index model.Index
	gen   model.Generator
}

func NewService(store model.Store, index model.Index, gen model.Generator) *Service {
	return &Service{
		store: store,
		index: index,
		gen:   gen,
	}
}

func (s *Service) Search(ctx context.Context, query model.SearchQuery) ([]model.SearchHit, error) {
	_ = ctx
	_ = query
	return nil, model.ErrNotImplemented
}

func (s *Service) Ask(ctx context.Context, question string, query model.SearchQuery) (model.AskResult, error) {
	_ = ctx
	_ = question
	_ = query
	return model.AskResult{}, model.ErrNotImplemented
}

func (s *Service) OpenFile(ctx context.Context, relPath string, span model.Span, maxChars int) (string, error) {
	_ = ctx
	_ = relPath
	_ = span
	_ = maxChars
	return "", model.ErrNotImplemented
}

func (s *Service) Stats(ctx context.Context) (model.Stats, error) {
	_ = ctx
	return model.Stats{}, model.ErrNotImplemented
}
