package cli

import (
	"context"
	"io"
	"math"
	"testing"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/model"
)

type fakeCorpusStore struct {
	docs []model.Document
}

func (f *fakeCorpusStore) Init(context.Context) error { return nil }
func (f *fakeCorpusStore) UpsertDocument(context.Context, model.Document) error {
	return nil
}
func (f *fakeCorpusStore) GetDocumentByPath(context.Context, string) (model.Document, error) {
	return model.Document{}, model.ErrNotImplemented
}
func (f *fakeCorpusStore) Close() error { return nil }

func (f *fakeCorpusStore) ListFiles(_ context.Context, _ string, _ string, limit, offset int) ([]model.Document, int64, error) {
	if offset >= len(f.docs) {
		return []model.Document{}, int64(len(f.docs)), nil
	}
	end := offset + limit
	if end > len(f.docs) {
		end = len(f.docs)
	}
	return append([]model.Document(nil), f.docs[offset:end]...), int64(len(f.docs)), nil
}

func TestBuildCorpusSnapshot_ComputesDocCountsAndCodeRatio(t *testing.T) {
	store := &fakeCorpusStore{
		docs: []model.Document{
			{RelPath: "src/a.go", DocType: "code", Deleted: false},
			{RelPath: "src/b.go", DocType: "code", Deleted: false},
			{RelPath: "docs/readme.md", DocType: "md", Deleted: false},
			{RelPath: "old/file.txt", DocType: "text", Deleted: true}, // excluded
		},
	}
	state := appstate.NewIndexingState(appstate.ModeIncremental)
	state.SetRunning(true)
	state.AddIndexed(3)

	snap, err := buildCorpusSnapshot(context.Background(), store, state, io.Discard, nil)
	if err != nil {
		t.Fatalf("buildCorpusSnapshot failed: %v", err)
	}
	if snap.TotalDocs != 3 {
		t.Errorf("expected total_docs=3, got %d", snap.TotalDocs)
	}
	if snap.DocCounts["code"] != 2 {
		t.Errorf("expected code count=2, got %d", snap.DocCounts["code"])
	}
	if snap.DocCounts["md"] != 1 {
		t.Errorf("expected md count=1, got %d", snap.DocCounts["md"])
	}
	// deleted documents should not be counted
	if snap.DocCounts["text"] != 0 {
		t.Errorf("deleted text docs should not be counted, got %d", snap.DocCounts["text"])
	}
	// use an epsilon-based comparison rather than hardcoded bounds
	eps := 1e-3
	if math.Abs(snap.CodeRatio-0.6667) > eps {
		t.Errorf("expected code_ratio around 0.6667 (±%f), got %f", eps, snap.CodeRatio)
	}
	if !snap.Indexing.Running {
		t.Errorf("expected indexing.running=true")
	}
	if snap.Indexing.Indexed != 3 {
		t.Errorf("expected indexing.indexed=3, got %d", snap.Indexing.Indexed)
	}
}

func TestBuildCorpusSnapshot_StatusCountsFallback(t *testing.T) {
	store := &fakeCorpusStore{
		docs: []model.Document{
			{DocType: "code", Status: "indexed"},
			{DocType: "md", Status: "skipped"},
			{DocType: "txt", Status: "error"},
			{DocType: "other", Status: "whatever"},
			{DocType: "code", Deleted: true, Status: "indexed"},
		},
	}

	snap, err := buildCorpusSnapshot(context.Background(), store, nil, io.Discard, nil)
	if err != nil {
		t.Fatalf("buildCorpusSnapshot failed: %v", err)
	}

	// deleted doc should not count toward TotalDocs or DocCounts
	if snap.TotalDocs != 4 {
		t.Errorf("expected total_docs=4, got %d", snap.TotalDocs)
	}
	if snap.DocCounts["code"] != 1 {
		t.Errorf("expected code count=1, got %d", snap.DocCounts["code"])
	}

	// status-derived indexing snapshot should reflect our test documents
	if snap.Indexing.Scanned != 5 {
		t.Errorf("expected scanned=5, got %d", snap.Indexing.Scanned)
	}
	if snap.Indexing.Indexed != 1 {
		t.Errorf("expected indexed=1 (deleted docs excluded even if status 'indexed'; only explicit 'indexed' or 'ok' statuses count), got %d", snap.Indexing.Indexed)
	}
	if snap.Indexing.Skipped != 1 {
		t.Errorf("expected skipped=1, got %d", snap.Indexing.Skipped)
	}
	if snap.Indexing.Errors != 1 {
		t.Errorf("expected errors=1, got %d", snap.Indexing.Errors)
	}
	if snap.Indexing.Deleted != 1 {
		t.Errorf("expected deleted=1, got %d", snap.Indexing.Deleted)
	}

	// passing nil for the live state should result in Running=false
	if snap.Indexing.Running {
		t.Errorf("expected indexing.running=false when live state is nil")
	}
}
