package store

import (
	"context"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type SQLiteStore struct {
	path string
}

func NewSQLiteStore(path string) *SQLiteStore {
	return &SQLiteStore{path: path}
}

func (s *SQLiteStore) Init(ctx context.Context) error {
	_ = ctx
	return model.ErrNotImplemented
}

func (s *SQLiteStore) UpsertDocument(ctx context.Context, doc model.Document) error {
	_ = ctx
	_ = doc
	return model.ErrNotImplemented
}

func (s *SQLiteStore) GetDocumentByPath(ctx context.Context, relPath string) (model.Document, error) {
	_ = ctx
	_ = relPath
	return model.Document{}, model.ErrNotImplemented
}

func (s *SQLiteStore) ListFiles(ctx context.Context, prefix, glob string, limit, offset int) ([]model.Document, int64, error) {
	_ = ctx
	_ = prefix
	_ = glob
	_ = limit
	_ = offset
	return nil, 0, model.ErrNotImplemented
}

func (s *SQLiteStore) Close() error {
	return nil
}
