package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	// when non-zero, InsertChunkWithSpans enforces this span count
	expectedSpanCount int
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
	// if a specific span count is expected, validate against it
	if s.expectedSpanCount != 0 && len(spans) != s.expectedSpanCount {
		return 0, fmt.Errorf("expected %d span(s), got %d", s.expectedSpanCount, len(spans))
	}

	s.chunks = append(s.chunks, chunk)
	// always append all provided spans so tests can inspect them
	s.spans = append(s.spans, spans...)
	return int64(len(s.chunks)), nil
}

func (s *fakeIngestStore) SoftDeleteChunksFromOrdinal(_ context.Context, _ int64, _ int) error {
	s.softDeleteCalls++
	return nil
}

// WithTx implements the model.RepresentationStore interface. A simple
// passthrough is sufficient for testing as the fake store does not maintain
// real transactional semantics.
func (s *fakeIngestStore) WithTx(ctx context.Context, fn func(tx model.RepresentationStore) error) error {
	return fn(s)
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

func TestReadOrComputeOCR_PrunesCacheByMaxBytes(t *testing.T) {
	stateDir := t.TempDir()
	svc := &Service{cfg: config.Config{StateDir: stateDir}}
	svc.SetOCRCacheLimits(10, 0)

	// create two cache entries, one old and one newer.  the combined size (11
	// bytes) is greater than our limit so the older file should be removed when
	// we call readOrComputeOCR.
	contentA := []byte("aaaaa")
	contentB := []byte("bbbbbb")
	pathA := filepath.Join(stateDir, "cache", "ocr", computeContentHash(contentA)+".md")
	pathB := filepath.Join(stateDir, "cache", "ocr", computeContentHash(contentB)+".md")
	if err := os.MkdirAll(filepath.Dir(pathA), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(pathA, []byte("aaaaa"), 0o644); err != nil {
		t.Fatalf("write file A: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(pathA, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("bbbbbb"), 0o644); err != nil {
		t.Fatalf("write file B: %v", err)
	}

	fake := &fakeOCR{text: "foo"}
	svc.ocr = fake
	doc := model.Document{RelPath: "docs/foo.pdf"}
	if _, err := svc.readOrComputeOCR(context.Background(), doc, []byte("ccc")); err != nil {
		t.Fatalf("readOrComputeOCR failed: %v", err)
	}

	if _, err := os.Stat(pathA); !os.IsNotExist(err) {
		t.Fatalf("expected oldest file removed, still exists: %v", err)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Fatalf("expected newer file kept: %v", err)
	}
}

func TestReadOrComputeOCR_PrunesCacheByTTL(t *testing.T) {
	stateDir := t.TempDir()
	svc := &Service{cfg: config.Config{StateDir: stateDir}}
	svc.SetOCRCacheLimits(0, time.Second)

	contentOld := []byte("old")
	pathOld := filepath.Join(stateDir, "cache", "ocr", computeContentHash(contentOld)+".md")
	if err := os.MkdirAll(filepath.Dir(pathOld), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(pathOld, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(pathOld, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}

	contentNew := []byte("new")
	pathNew := filepath.Join(stateDir, "cache", "ocr", computeContentHash(contentNew)+".md")
	if err := os.WriteFile(pathNew, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	fake := &fakeOCR{text: "bar"}
	svc.ocr = fake
	doc := model.Document{RelPath: "docs/foo.pdf"}
	if _, err := svc.readOrComputeOCR(context.Background(), doc, []byte("ccc")); err != nil {
		t.Fatalf("readOrComputeOCR failed: %v", err)
	}

	if _, err := os.Stat(pathOld); !os.IsNotExist(err) {
		t.Fatalf("expected old TTL file removed: %v", err)
	}
	if _, err := os.Stat(pathNew); err != nil {
		t.Fatalf("expected new file kept: %v", err)
	}
}

func TestReadOrComputeOCR_PrunesCacheByTTLThenSize(t *testing.T) {
	stateDir := t.TempDir()
	svc := &Service{cfg: config.Config{StateDir: stateDir}}
	svc.SetOCRCacheLimits(10, time.Second)

	cacheDir := filepath.Join(stateDir, "cache", "ocr")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}

	contentOldA := []byte("old-A")
	contentOldB := []byte("old-B")
	contentNewA := []byte("new-A")
	contentNewB := []byte("new-B")
	pathOldA := filepath.Join(cacheDir, computeContentHash(contentOldA)+".md")
	pathOldB := filepath.Join(cacheDir, computeContentHash(contentOldB)+".md")
	pathNewA := filepath.Join(cacheDir, computeContentHash(contentNewA)+".md")
	pathNewB := filepath.Join(cacheDir, computeContentHash(contentNewB)+".md")

	for _, tc := range []struct {
		path string
		data string
	}{
		{path: pathOldA, data: "12345"},
		{path: pathOldB, data: "67890"},
		{path: pathNewA, data: "abcde"},
		{path: pathNewB, data: "fghij"},
	} {
		if err := os.WriteFile(tc.path, []byte(tc.data), 0o644); err != nil {
			t.Fatalf("write cache file %s: %v", tc.path, err)
		}
	}

	oldTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(pathOldA, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old A: %v", err)
	}
	if err := os.Chtimes(pathOldB, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old B: %v", err)
	}
	newer := time.Now().Add(-200 * time.Millisecond)
	newest := time.Now().Add(-100 * time.Millisecond)
	if err := os.Chtimes(pathNewA, newer, newer); err != nil {
		t.Fatalf("chtimes new A: %v", err)
	}
	if err := os.Chtimes(pathNewB, newest, newest); err != nil {
		t.Fatalf("chtimes new B: %v", err)
	}

	svc.ocr = &fakeOCR{text: "should not be used"}
	doc := model.Document{RelPath: "docs/foo.pdf"}
	got, err := svc.readOrComputeOCR(context.Background(), doc, contentNewB)
	if err != nil {
		t.Fatalf("readOrComputeOCR failed: %v", err)
	}
	if got != "fghij" {
		t.Fatalf("expected cache hit for newest entry, got %q", got)
	}

	if _, err := os.Stat(pathOldA); !os.IsNotExist(err) {
		t.Fatalf("expected old A removed by TTL, got %v", err)
	}
	if _, err := os.Stat(pathOldB); !os.IsNotExist(err) {
		t.Fatalf("expected old B removed by TTL, got %v", err)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("readdir cache dir: %v", err)
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatalf("stat cache entry %s: %v", e.Name(), err)
		}
		total += info.Size()
	}
	if total > 10 {
		t.Fatalf("expected cache total <= 10 after TTL+size prune, got %d", total)
	}
}

func TestClearOCRCache(t *testing.T) {
	stateDir := t.TempDir()
	svc := &Service{cfg: config.Config{StateDir: stateDir}}
	// seed one file
	if err := os.MkdirAll(filepath.Join(stateDir, "cache", "ocr"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(stateDir, "cache", "ocr", "foo.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := svc.ClearOCRCache(); err != nil {
		t.Fatalf("ClearOCRCache failed: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("expected cache dir removed, got %v", err)
	}
}

func TestFakeIngestStore_InsertChunkWithSpansExpectation(t *testing.T) {
	st := &fakeIngestStore{expectedSpanCount: 2}
	chunk := model.Chunk{ChunkID: 1, Text: "foo"}
	spans := []model.Span{{Kind: "a"}, {Kind: "b"}}
	if _, err := st.InsertChunkWithSpans(context.Background(), chunk, spans); err != nil {
		t.Fatalf("expected success with 2 spans: %v", err)
	}
	if len(st.chunks) != 1 {
		t.Fatalf("chunk not recorded")
	}
	if len(st.spans) != 2 {
		t.Fatalf("span count not recorded, got %d", len(st.spans))
	}

	// now expect mismatch error
	st = &fakeIngestStore{expectedSpanCount: 3}
	if _, err := st.InsertChunkWithSpans(context.Background(), chunk, spans); err == nil {
		t.Fatalf("expected error when span count mismatch")
	}
}
