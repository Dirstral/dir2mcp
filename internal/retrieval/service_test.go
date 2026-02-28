package retrieval

import (
	"context"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/index"
	"github.com/Dirstral/dir2mcp/internal/model"
)

type fakeEmbedder struct {
	vectorsByModel map[string][]float32
}

func (e *fakeEmbedder) Embed(_ context.Context, model string, _ []string) ([][]float32, error) {
	if vec, ok := e.vectorsByModel[model]; ok {
		return [][]float32{vec}, nil
	}
	return [][]float32{{1, 0}}, nil
}

func TestSearch_ReturnsRankedHitsWithFilters(t *testing.T) {
	idx := index.NewHNSWIndex("")
	_ = idx.Add(1, []float32{1, 0})
	_ = idx.Add(2, []float32{0.9, 0.1})
	_ = idx.Add(3, []float32{0, 1})

	svc := NewService(nil, idx, &fakeEmbedder{vectorsByModel: map[string][]float32{
		"mistral-embed":   {1, 0},
		"codestral-embed": {0, 1},
	}}, nil)
	svc.SetChunkMetadata(1, model.SearchHit{RelPath: "docs/a.md", DocType: "md", Snippet: "alpha"})
	svc.SetChunkMetadata(2, model.SearchHit{RelPath: "src/main.go", DocType: "code", Snippet: "beta"})
	svc.SetChunkMetadata(3, model.SearchHit{RelPath: "docs/b.md", DocType: "md", Snippet: "gamma"})

	hits, err := svc.Search(context.Background(), model.SearchQuery{
		Query:      "alpha",
		K:          2,
		PathPrefix: "docs/",
		FileGlob:   "docs/a.*",
		DocTypes:   []string{"md"},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 filtered hit, got %d", len(hits))
	}
	if hits[0].RelPath != "docs/a.md" {
		t.Fatalf("unexpected top hit: %#v", hits[0])
	}
}

func TestSearch_FileGlobFilter(t *testing.T) {
	idx := index.NewHNSWIndex("")
	_ = idx.Add(10, []float32{1, 0})
	_ = idx.Add(20, []float32{0.8, 0.2})

	svc := NewService(nil, idx, &fakeEmbedder{vectorsByModel: map[string][]float32{
		"mistral-embed":   {1, 0},
		"codestral-embed": {0, 1},
	}}, nil)
	svc.SetChunkMetadata(10, model.SearchHit{RelPath: "src/a.go", DocType: "code"})
	svc.SetChunkMetadata(20, model.SearchHit{RelPath: "docs/a.md", DocType: "md"})

	hits, err := svc.Search(context.Background(), model.SearchQuery{
		Query:    "q",
		K:        5,
		FileGlob: "src/*.go",
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 1 || hits[0].RelPath != "src/a.go" {
		t.Fatalf("unexpected glob filtered hits: %#v", hits)
	}
}

func TestSearch_BothMode_DedupesAndNormalizes(t *testing.T) {
	textIdx := index.NewHNSWIndex("")
	codeIdx := index.NewHNSWIndex("")
	_ = textIdx.Add(1, []float32{1, 0})
	_ = textIdx.Add(2, []float32{0.8, 0.2})
	_ = codeIdx.Add(2, []float32{0.9, 0.1}) // duplicate label across indexes
	_ = codeIdx.Add(3, []float32{1, 0})

	svc := NewService(nil, textIdx, &fakeEmbedder{vectorsByModel: map[string][]float32{
		"mistral-embed":   {1, 0},
		"codestral-embed": {1, 0},
	}}, nil)
	svc.SetCodeIndex(codeIdx)
	svc.SetChunkMetadata(1, model.SearchHit{RelPath: "docs/1.md", DocType: "md"})
	svc.SetChunkMetadata(2, model.SearchHit{RelPath: "src/2.go", DocType: "code"})
	svc.SetChunkMetadata(3, model.SearchHit{RelPath: "src/3.go", DocType: "code"})

	hits, err := svc.Search(context.Background(), model.SearchQuery{
		Query: "query",
		K:     10,
		Index: "both",
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 deduped hits, got %d", len(hits))
	}
	seen := map[int64]bool{}
	for _, hit := range hits {
		if seen[hit.ChunkID] {
			t.Fatalf("duplicate chunk in merged results: %d", hit.ChunkID)
		}
		seen[hit.ChunkID] = true
		if hit.Score < 0 || hit.Score > 1 {
			t.Fatalf("score should be normalized to [0,1], got %f", hit.Score)
		}
	}
}
