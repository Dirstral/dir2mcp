package retrieval

import (
	"context"
	"math"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/index"
	"github.com/Dirstral/dir2mcp/internal/model"
)

type fakeEmbedder struct {
	vectorsByModel map[string][]float32
}

// fakeIndex allows inspection of the 'k' value passed to Search.
type fakeIndex struct {
	lastK int
}

func (f *fakeIndex) Add(label uint64, vector []float32) error { return nil }
func (f *fakeIndex) Search(vector []float32, k int) ([]uint64, []float32, error) {
	f.lastK = k
	return []uint64{}, []float32{}, nil
}
func (f *fakeIndex) Save(path string) error { return nil }
func (f *fakeIndex) Load(path string) error { return nil }
func (f *fakeIndex) Close() error           { return nil }

func (e *fakeEmbedder) Embed(_ context.Context, model string, texts []string) ([][]float32, error) {
	// return one embedding per input text, matching the real embedder behaviour
	n := len(texts)
	if n == 0 {
		return [][]float32{}, nil
	}
	var vec []float32
	if v, ok := e.vectorsByModel[model]; ok {
		vec = v
	} else {
		vec = []float32{1, 0}
	}
	res := make([][]float32, n)
	for i := range res {
		res[i] = vec
	}
	return res, nil
}

func TestSearch_ReturnsRankedHitsWithFilters(t *testing.T) {
	idx := index.NewHNSWIndex("")
	if err := idx.Add(1, []float32{1, 0}); err != nil {
		t.Fatalf("idx.Add failed: %v", err)
	}
	if err := idx.Add(2, []float32{0.9, 0.1}); err != nil {
		t.Fatalf("idx.Add failed: %v", err)
	}
	if err := idx.Add(3, []float32{0, 1}); err != nil {
		t.Fatalf("idx.Add failed: %v", err)
	}

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
	if err := idx.Add(10, []float32{1, 0}); err != nil {
		t.Fatalf("idx.Add failed: %v", err)
	}
	if err := idx.Add(20, []float32{0.8, 0.2}); err != nil {
		t.Fatalf("idx.Add failed: %v", err)
	}

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

func TestSearch_OverfetchMultiplier_DefaultAndConfigurable(t *testing.T) {
	fi := &fakeIndex{}
	svc := NewService(nil, fi, &fakeEmbedder{vectorsByModel: map[string][]float32{}}, nil)
	// first search should use default multiplier (5)
	if _, err := svc.Search(context.Background(), model.SearchQuery{Query: "x", K: 3}); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if fi.lastK != 3*5 {
		t.Fatalf("expected default overfetch 5x (got %d)", fi.lastK)
	}
	// change multiplier to a smaller value and verify it takes effect
	svc.SetOverfetchMultiplier(2)
	if _, err := svc.Search(context.Background(), model.SearchQuery{Query: "x", K: 3}); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if fi.lastK != 3*2 {
		t.Fatalf("expected overfetch 2x after set (got %d)", fi.lastK)
	}
	// invalid values should be normalized
	svc.SetOverfetchMultiplier(0)
	if _, err := svc.Search(context.Background(), model.SearchQuery{Query: "x", K: 1}); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if fi.lastK != 1*1 {
		t.Fatalf("multiplier lower bound not enforced (got %d)", fi.lastK)
	}
	// extremely large value should be capped
	svc.SetOverfetchMultiplier(1000)
	if _, err := svc.Search(context.Background(), model.SearchQuery{Query: "x", K: 1}); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if fi.lastK > 100*1 {
		t.Fatalf("multiplier upper cap not enforced (got %d)", fi.lastK)
	}
}

// ensure we don't overflow when k * multiplier would exceed the range of
// int.  The actual index implementation can't handle a number this large
// anyway, so we expect the result to be clamped to math.MaxInt.
func TestSearch_OverflowProtection(t *testing.T) {
	fi := &fakeIndex{}
	svc := NewService(nil, fi, &fakeEmbedder{vectorsByModel: map[string][]float32{}}, nil)
	// set multiplier to max allowed by the setter; the service doesn't crash
	svc.SetOverfetchMultiplier(100)
	// choose a k that's guaranteed to overflow when multiplied by 100
	bigK := math.MaxInt/100 + 1
	if _, err := svc.Search(context.Background(), model.SearchQuery{Query: "x", K: bigK}); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if fi.lastK != math.MaxInt {
		t.Fatalf("expected clamped value math.MaxInt (%d), got %d", math.MaxInt, fi.lastK)
	}
}

func TestSearch_BothMode_DedupesAndNormalizes(t *testing.T) {
	textIdx := index.NewHNSWIndex("")
	codeIdx := index.NewHNSWIndex("")
	if err := textIdx.Add(1, []float32{1, 0}); err != nil {
		t.Fatalf("textIdx.Add failed: %v", err)
	}
	if err := textIdx.Add(2, []float32{0.8, 0.2}); err != nil {
		t.Fatalf("textIdx.Add failed: %v", err)
	}
	if err := codeIdx.Add(2, []float32{0.9, 0.1}); err != nil { // duplicate label across indexes
		t.Fatalf("codeIdx.Add failed: %v", err)
	}
	if err := codeIdx.Add(3, []float32{1, 0}); err != nil {
		t.Fatalf("codeIdx.Add failed: %v", err)
	}

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

func TestLooksLikeCodeQuery(t *testing.T) {
	cases := []struct {
		query  string
		expect bool
	}{
		{"func main() {}", true},
		{"I love python", false},
		{"go to the store", false},
		{"use import and ([])", true},
		{"I wrote code in .go file", false},
		{"I wrote code in .go file `snippet`", true},
		{"this is just plain text", false},
		{"```go\nfmt.Println(\"hi\")\n```", true},
		{"some `inline code`", false},
		{"python code", false},
		{"fix bug in java { }", false},
	}

	for _, c := range cases {
		got := looksLikeCodeQuery(c.query)
		if got != c.expect {
			t.Errorf("looksLikeCodeQuery(%q) = %v; want %v", c.query, got, c.expect)
		}
	}
}
