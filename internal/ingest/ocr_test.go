package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

type fakeOCR struct {
	text  string
	err   error
	calls int
}

func (f *fakeOCR) Extract(_ context.Context, _ string, _ []byte) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

type fakeIngestStore struct {
	reps            []model.Representation
	chunks          []model.Chunk
	spans           []model.Span
	softDeleteCalls int
}

func (s *fakeIngestStore) Init(context.Context) error { return nil }
func (s *fakeIngestStore) UpsertDocument(context.Context, model.Document) error {
	return nil
}
func (s *fakeIngestStore) GetDocumentByPath(context.Context, string) (model.Document, error) {
	return model.Document{}, os.ErrNotExist
}
func (s *fakeIngestStore) ListFiles(context.Context, string, string, int, int) ([]model.Document, int64, error) {
	return nil, 0, nil
}
func (s *fakeIngestStore) Close() error { return nil }

func (s *fakeIngestStore) UpsertRepresentation(_ context.Context, rep model.Representation) (int64, error) {
	s.reps = append(s.reps, rep)
	return int64(len(s.reps)), nil
}

func (s *fakeIngestStore) InsertChunkWithSpans(_ context.Context, chunk model.Chunk, spans []model.Span) (int64, error) {
	if len(spans) != 1 {
		return 0, errors.New("expected one span")
	}
	s.chunks = append(s.chunks, chunk)
	s.spans = append(s.spans, spans[0])
	return int64(len(s.chunks)), nil
}

func (s *fakeIngestStore) SoftDeleteChunksFromOrdinal(_ context.Context, _ int64, _ int) error {
	s.softDeleteCalls++
	return nil
}

func TestGenerateOCRMarkdownRepresentation_PersistsPagedChunks(t *testing.T) {
	stateDir := t.TempDir()
	st := &fakeIngestStore{}
	svc := &Service{
		cfg:    config.Config{StateDir: stateDir},
		repGen: NewRepresentationGenerator(st),
		ocr:    &fakeOCR{text: "page-1 text\fpage-2 text"},
	}

	doc := model.Document{
		DocID:   42,
		RelPath: "docs/spec.pdf",
		DocType: "pdf",
	}
	content := []byte("pdf bytes")

	if err := svc.generateOCRMarkdownRepresentation(context.Background(), doc, content); err != nil {
		t.Fatalf("generateOCRMarkdownRepresentation failed: %v", err)
	}

	if len(st.reps) != 1 {
		t.Fatalf("expected one representation, got %d", len(st.reps))
	}
	if st.reps[0].RepType != RepTypeOCRMarkdown {
		t.Fatalf("expected rep type %q, got %q", RepTypeOCRMarkdown, st.reps[0].RepType)
	}
	if len(st.chunks) != 2 {
		t.Fatalf("expected two OCR chunks, got %d", len(st.chunks))
	}
	if st.spans[0].Kind != "page" || st.spans[0].Page != 1 {
		t.Fatalf("unexpected first page span: %+v", st.spans[0])
	}
	if st.spans[1].Kind != "page" || st.spans[1].Page != 2 {
		t.Fatalf("unexpected second page span: %+v", st.spans[1])
	}
	if st.softDeleteCalls == 0 {
		t.Fatalf("expected stale chunk cleanup call")
	}

	cachePath := filepath.Join(stateDir, "cache", "ocr", computeContentHash(content)+".md")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected OCR cache file at %s: %v", cachePath, err)
	}
}

func TestReadOrComputeOCR_UsesCache(t *testing.T) {
	stateDir := t.TempDir()
	content := []byte("same bytes")
	cachePath := filepath.Join(stateDir, "cache", "ocr", computeContentHash(content)+".md")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("cached ocr"), 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	ocr := &fakeOCR{text: "fresh ocr"}
	svc := &Service{
		cfg: config.Config{StateDir: stateDir},
		ocr: ocr,
	}
	doc := model.Document{RelPath: "docs/spec.pdf"}

	got, err := svc.readOrComputeOCR(context.Background(), doc, content)
	if err != nil {
		t.Fatalf("readOrComputeOCR failed: %v", err)
	}
	if got != "cached ocr" {
		t.Fatalf("expected cached value, got %q", got)
	}
	if ocr.calls != 0 {
		t.Fatalf("expected OCR provider not to be called, got %d calls", ocr.calls)
	}
}
