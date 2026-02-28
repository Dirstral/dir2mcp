package ingest_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dir2mcp/internal/ingest"
	"dir2mcp/internal/model"
)

func TestNewRepresentationGeneratorNil(t *testing.T) {
	// ensure constructor fails early when given a nil store
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for nil store")
		} else if !strings.Contains(fmt.Sprint(r), "nil model.RepresentationStore") {
			t.Fatalf("unexpected panic message: %v", r)
		}
	}()
	_ = ingest.NewRepresentationGenerator(nil)
}

func TestNormalizeUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "already valid UTF-8 with LF",
			input:    []byte("hello\nworld"),
			expected: []byte("hello\nworld"),
		},
		{
			name:     "CRLF to LF",
			input:    []byte("hello\r\nworld"),
			expected: []byte("hello\nworld"),
		},
		{
			name:     "CR to LF",
			input:    []byte("hello\rworld"),
			expected: []byte("hello\nworld"),
		},
		{
			name:     "mixed line endings",
			input:    []byte("line1\r\nline2\rline3\nline4"),
			expected: []byte("line1\nline2\nline3\nline4"),
		},
		{
			name:     "empty content",
			input:    []byte{},
			expected: []byte{},
		},
		{
			name:     "valid UTF-8 with special chars",
			input:    []byte("Hello 世界 🌍"),
			expected: []byte("Hello 世界 🌍"),
		},
		{
			name:  "invalid UTF-8 sequence",
			input: []byte{0xff, 0xfe, 0x00},
			// strings.ToValidUTF8 treats consecutive invalid bytes (0xff, 0xfe)
			// as a single invalid sequence and replaces them with one U+FFFD
			// (0xEF,0xBF,0xBD); trailing 0x00 is preserved.
			expected: []byte{0xEF, 0xBF, 0xBD, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ingest.NormalizeUTF8(tt.input)
			if !bytes.Equal(result, tt.expected) {
				t.Errorf("ingest.NormalizeUTF8() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestShouldGenerateRawText(t *testing.T) {
	tests := []struct {
		name     string
		docType  string
		expected bool
	}{
		// Should generate raw_text
		{"code", "code", true},
		{"text", "text", true},
		{"markdown", "md", true},
		{"data", "data", true},
		{"html", "html", true},

		// Should NOT generate raw_text
		{"pdf", "pdf", false},
		{"image", "image", false},
		{"audio", "audio", false},
		{"archive", "archive", false},
		{"binary_ignored", "binary_ignored", false},
		{"unknown", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ingest.ShouldGenerateRawText(tt.docType)
			if result != tt.expected {
				t.Errorf("ingest.ShouldGenerateRawText(%q) = %v, want %v", tt.docType, result, tt.expected)
			}
		})
	}
}

func TestRepTypeConstants(t *testing.T) {
	// Verify constants are defined with expected values per SPEC
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"raw_text", ingest.RepTypeRawText, "raw_text"},
		{"ocr_markdown", ingest.RepTypeOCRMarkdown, "ocr_markdown"},
		{"transcript", ingest.RepTypeTranscript, "transcript"},
		{"annotation_json", ingest.RepTypeAnnotationJSON, "annotation_json"},
		{"annotation_text", ingest.RepTypeAnnotationText, "annotation_text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("Constant %s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// Example integration test structure (implementation would be in a separate file)
func TestRepresentationGeneratorIntegration(t *testing.T) {
	st := &fakeRepStore{failAfter: -1}
	rg := ingest.NewRepresentationGenerator(st)
	doc := model.Document{
		DocID:   1,
		RelPath: "main.go",
		DocType: "code",
	}

	tmp := filepath.Join(t.TempDir(), "main.go")
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if err := rg.GenerateRawText(context.Background(), doc, tmp); err != nil {
		t.Fatalf("GenerateRawText failed: %v", err)
	}
	if st.upsertCount != 1 {
		t.Fatalf("expected 1 representation upsert, got %d", st.upsertCount)
	}
	if len(st.chunks) == 0 {
		t.Fatalf("expected chunks to be inserted")
	}
	if st.softDeleteCall == 0 {
		t.Fatalf("expected stale-chunk cleanup call")
	}
}

func TestGenerateRawTextFromContentPrefersGivenBytes(t *testing.T) {
	st := &fakeRepStore{failAfter: -1}
	rg := ingest.NewRepresentationGenerator(st)
	doc := model.Document{DocID: 1, RelPath: "foo.txt", DocType: "text"}

	provided := []byte("provided content")
	if err := rg.GenerateRawTextFromContent(context.Background(), doc, provided); err != nil {
		t.Fatalf("GenerateRawTextFromContent failed: %v", err)
	}
	if st.upsertCount != 1 {
		t.Fatalf("expected 1 representation upsert, got %d", st.upsertCount)
	}
	// compute hash of provided content to ensure it was used
	hash := ingest.ComputeRepHash(ingest.NormalizeUTF8(provided))
	if len(st.reps) == 0 {
		t.Fatalf("no representation recorded, expected at least one")
	}
	if st.reps[0].RepHash != hash {
		t.Fatalf("representation hash %q does not match provided content hash %q", st.reps[0].RepHash, hash)
	}
}

func TestGenerateRawTextTooLarge(t *testing.T) {
	// use failAfter=-1 to ensure the fake store does not inject failures for
	// this test, which only verifies oversized-file rejection before any
	// chunks are inserted.
	st := &fakeRepStore{failAfter: -1}
	rg := ingest.NewRepresentationGenerator(st)
	doc := model.Document{DocID: 1, RelPath: "large.txt", DocType: "text"}

	// create a file just above the defaultMaxFileSizeBytes limit
	tmp := filepath.Join(t.TempDir(), "large.txt")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if err := f.Truncate(ingest.DefaultMaxFileSizeBytes() + 1); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("close temp file after truncate failure: %v", closeErr)
		}
		t.Fatalf("truncate file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	err = rg.GenerateRawText(context.Background(), doc, tmp)
	if err == nil {
		t.Fatalf("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestChunkCodeByLines(t *testing.T) {
	content := strings.Repeat("line\n", 260)
	chunks := ingest.ChunkCodeByLines(content, 200, 30)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Span.Kind != "lines" {
		t.Fatalf("expected lines span kind, got %q", chunks[0].Span.Kind)
	}
	if chunks[0].Span.StartLine != 1 || chunks[0].Span.EndLine != 200 {
		t.Fatalf("unexpected first chunk span: %+v", chunks[0].Span)
	}
	if chunks[1].Span.StartLine != 171 {
		t.Fatalf("expected overlap start line 171, got %d", chunks[1].Span.StartLine)
	}
}

func TestChunkTextByChars(t *testing.T) {
	content := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 200)
	chunks := ingest.ChunkTextByChars(content, 250, 25, 50)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]rune(c.Text)) > 250 {
			t.Fatalf("chunk %d exceeds max chars: %d", i, len([]rune(c.Text)))
		}
		if c.Span.Kind != "lines" {
			t.Fatalf("chunk %d has unexpected span kind %q", i, c.Span.Kind)
		}
	}
}

type fakeRepStore struct {
	upsertCount int
	nextRepID   int64
	reps        []model.Representation
	chunks      []model.Chunk
	// store all spans that have been passed in so tests can inspect them
	spans []model.Span
	// when non-zero, InsertChunkWithSpans will enforce this span count
	expectedSpanCount int
	softDeleteCall    int
	// failAfter simulates a failure after inserting this many chunks (0-based)
	// negative means never fail.
	failAfter int
}

func (s *fakeRepStore) UpsertRepresentation(_ context.Context, rep model.Representation) (int64, error) {
	s.upsertCount++
	if s.nextRepID == 0 {
		s.nextRepID = 1
	}
	if rep.DocID <= 0 {
		return 0, fmt.Errorf("invalid doc id")
	}
	// record rep for later inspection
	s.reps = append(s.reps, rep)
	currentID := s.nextRepID
	s.nextRepID++
	return currentID, nil
}

func (s *fakeRepStore) InsertChunkWithSpans(_ context.Context, chunk model.Chunk, spans []model.Span) (int64, error) {
	if chunk.RepID <= 0 {
		return 0, fmt.Errorf("invalid rep id")
	}
	// simulate failure injection before doing any work
	if s.failAfter >= 0 && len(s.chunks) == s.failAfter {
		return 0, fmt.Errorf("injected failure at chunk %d", s.failAfter)
	}
	// if an expected span count has been configured, enforce it
	if s.expectedSpanCount != 0 && len(spans) != s.expectedSpanCount {
		return 0, fmt.Errorf("expected %d span(s), got %d", s.expectedSpanCount, len(spans))
	}
	// require at least one span
	if len(spans) < 1 {
		return 0, fmt.Errorf("expected at least one span")
	}

	// record the chunk and all provided spans so later assertions can examine them
	s.chunks = append(s.chunks, chunk)
	s.spans = append(s.spans, spans...)
	return int64(len(s.chunks)), nil
}

func (s *fakeRepStore) SoftDeleteChunksFromOrdinal(_ context.Context, repID int64, fromOrdinal int) error {
	if repID <= 0 || fromOrdinal < 0 {
		return fmt.Errorf("invalid soft delete args")
	}
	s.softDeleteCall++
	return nil
}

// WithTx implements a very basic transaction emulation.  We snapshot mutable
// fields and restore them if the callback returns an error, effectively
// rolling back.  Initially we only needed to track chunks/spans/soft deletes,
// but later tests can inspect representations as well so we must ensure
// UpsertRepresentation mutations also roll back.
func (s *fakeRepStore) WithTx(ctx context.Context, fn func(tx model.RepresentationStore) error) error {
	origChunks := append([]model.Chunk(nil), s.chunks...)
	origSpans := append([]model.Span(nil), s.spans...)
	origSoft := s.softDeleteCall
	origUpsert := s.upsertCount
	origReps := append([]model.Representation(nil), s.reps...)
	origNext := s.nextRepID

	err := fn(s)
	if err != nil {
		s.chunks = origChunks
		s.spans = origSpans
		s.softDeleteCall = origSoft
		s.upsertCount = origUpsert
		s.reps = origReps
		s.nextRepID = origNext
	}
	return err
}

func TestUpsertChunksForRepresentationTransaction(t *testing.T) {
	st := &fakeRepStore{failAfter: 1}
	rg := ingest.NewRepresentationGenerator(st)
	doc := model.Document{DocID: 42, RelPath: "main.go", DocType: "code"}
	content := []byte(strings.Repeat("line\n", 260))
	err := rg.GenerateRawTextFromContent(context.Background(), doc, content)
	if err == nil {
		t.Fatal("expected error from failing chunk insert")
	}
	if len(st.chunks) != 0 {
		t.Fatalf("expected no chunks after rollback, got %d", len(st.chunks))
	}
	if st.softDeleteCall != 0 {
		t.Fatalf("expected no soft-delete call after rollback")
	}
}
