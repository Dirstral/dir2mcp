package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

type fakeIncrementalStore struct {
	existingDoc      model.Document
	existingErr      error
	upsertDocCalls   int
	upsertRepCalls   int
	insertChunkCalls int
}

func (f *fakeIncrementalStore) Init(context.Context) error { return nil }
func (f *fakeIncrementalStore) Close() error               { return nil }
func (f *fakeIncrementalStore) ListFiles(context.Context, string, string, int, int) ([]model.Document, int64, error) {
	return nil, 0, nil
}
func (f *fakeIncrementalStore) UpsertDocument(_ context.Context, _ model.Document) error {
	f.upsertDocCalls++
	return nil
}
func (f *fakeIncrementalStore) GetDocumentByPath(_ context.Context, _ string) (model.Document, error) {
	if f.existingErr != nil {
		return model.Document{}, f.existingErr
	}
	return f.existingDoc, nil
}
func (f *fakeIncrementalStore) UpsertRepresentation(_ context.Context, _ model.Representation) (int64, error) {
	f.upsertRepCalls++
	return 1, nil
}
func (f *fakeIncrementalStore) InsertChunkWithSpans(_ context.Context, _ model.Chunk, _ []model.Span) (int64, error) {
	f.insertChunkCalls++
	return int64(f.insertChunkCalls), nil
}
func (f *fakeIncrementalStore) SoftDeleteChunksFromOrdinal(context.Context, int64, int) error {
	return nil
}

func TestProcessDocument_IncrementalSkipsUnchangedRepresentation(t *testing.T) {
	root := t.TempDir()
	absPath := filepath.Join(root, "a.txt")
	if err := os.WriteFile(absPath, []byte("same-content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	hash := computeContentHash([]byte("same-content"))

	st := &fakeIncrementalStore{
		existingDoc: model.Document{
			DocID:       10,
			RelPath:     "a.txt",
			ContentHash: hash,
		},
	}
	svc := NewService(config.Config{RootDir: root}, st)
	df := DiscoveredFile{
		AbsPath:   absPath,
		RelPath:   "a.txt",
		SizeBytes: int64(len("same-content")),
	}
	if err := svc.processDocument(context.Background(), df, nil, false); err != nil {
		t.Fatalf("processDocument failed: %v", err)
	}

	if st.upsertDocCalls != 1 {
		t.Fatalf("expected one document upsert, got %d", st.upsertDocCalls)
	}
	if st.upsertRepCalls != 0 {
		t.Fatalf("expected representation generation to be skipped, got %d upserts", st.upsertRepCalls)
	}
}

func TestProcessDocument_ForceReindexRegeneratesRepresentation(t *testing.T) {
	root := t.TempDir()
	absPath := filepath.Join(root, "main.go")
	content := "package main\n\nfunc main(){}\n"
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	hash := computeContentHash([]byte(content))

	st := &fakeIncrementalStore{
		existingDoc: model.Document{
			DocID:       11,
			RelPath:     "main.go",
			ContentHash: hash,
		},
	}
	svc := NewService(config.Config{RootDir: root}, st)
	df := DiscoveredFile{
		AbsPath:   absPath,
		RelPath:   "main.go",
		SizeBytes: int64(len(content)),
	}
	if err := svc.processDocument(context.Background(), df, nil, true); err != nil {
		t.Fatalf("processDocument failed: %v", err)
	}

	if st.upsertRepCalls == 0 {
		t.Fatalf("expected representation to be regenerated in force mode")
	}
	if st.insertChunkCalls == 0 {
		t.Fatalf("expected chunk persistence during regeneration")
	}
}
