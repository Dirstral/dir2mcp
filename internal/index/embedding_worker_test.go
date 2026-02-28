package index

import (
	"context"
	"errors"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type fakeChunkSource struct {
	tasks        []model.ChunkTask
	embedded     []uint64
	failedLabels []uint64
	failedReason string
}

func (s *fakeChunkSource) NextPending(_ context.Context, _ int, _ string) ([]model.ChunkTask, error) {
	out := s.tasks
	s.tasks = nil
	return out, nil
}

func (s *fakeChunkSource) MarkEmbedded(_ context.Context, labels []uint64) error {
	s.embedded = append(s.embedded, labels...)
	return nil
}

func (s *fakeChunkSource) MarkFailed(_ context.Context, labels []uint64, reason string) error {
	s.failedLabels = append(s.failedLabels, labels...)
	s.failedReason = reason
	return nil
}

type fakeEmbedder struct {
	vectors [][]float32
	err     error
}

func (e *fakeEmbedder) Embed(_ context.Context, _ string, _ []string) ([][]float32, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.vectors, nil
}

func TestEmbeddingWorker_RunOnce_Success(t *testing.T) {
	source := &fakeChunkSource{
		tasks: []model.ChunkTask{
			{Label: 11, Text: "alpha", Metadata: model.SearchHit{RelPath: "a.txt", DocType: "text"}},
			{Label: 22, Text: "beta", Metadata: model.SearchHit{RelPath: "b.go", DocType: "code"}},
		},
	}

	idx := NewHNSWIndex("")
	embedder := &fakeEmbedder{
		vectors: [][]float32{
			{1, 0},
			{0, 1},
		},
	}

	indexed := make(map[uint64]model.SearchHit)
	worker := &EmbeddingWorker{
		Source:       source,
		Index:        idx,
		Embedder:     embedder,
		BatchSize:    2,
		ModelForText: "mistral-embed",
		OnIndexedChunk: func(label uint64, metadata model.SearchHit) {
			indexed[label] = metadata
		},
	}

	n, err := worker.RunOnce(context.Background(), "text")
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("unexpected indexed count: %d", n)
	}
	if len(source.embedded) != 2 {
		t.Fatalf("expected 2 embedded labels, got %d", len(source.embedded))
	}
	if indexed[11].RelPath != "a.txt" || indexed[22].RelPath != "b.go" {
		t.Fatalf("metadata callback mismatch: %#v", indexed)
	}
}

func TestEmbeddingWorker_RunOnce_EmbeddingFailure(t *testing.T) {
	source := &fakeChunkSource{
		tasks: []model.ChunkTask{
			{Label: 99, Text: "fail"},
		},
	}

	worker := &EmbeddingWorker{
		Source:   source,
		Index:    NewHNSWIndex(""),
		Embedder: &fakeEmbedder{err: errors.New("upstream failed")},
	}

	n, err := worker.RunOnce(context.Background(), "text")
	if err == nil {
		t.Fatal("expected error")
	}
	if n != 0 {
		t.Fatalf("unexpected indexed count: %d", n)
	}
	if len(source.failedLabels) != 1 || source.failedLabels[0] != 99 {
		t.Fatalf("expected failed label 99, got %#v", source.failedLabels)
	}
}
