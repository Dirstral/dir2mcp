package retrieval

import (
	"context"
	"errors"
	"testing"

	"dir2mcp/internal/model"
)

type fakeEngineRetriever struct {
	result      model.AskResult
	err         error
	gotQuestion string
	gotQuery    model.SearchQuery
}

func (f *fakeEngineRetriever) Ask(_ context.Context, question string, query model.SearchQuery) (model.AskResult, error) {
	f.gotQuestion = question
	f.gotQuery = query
	if f.err != nil {
		return model.AskResult{}, f.err
	}
	return f.result, nil
}

func TestEngineAsk_UsesRetrieverAndMapsCitations(t *testing.T) {
	retriever := &fakeEngineRetriever{
		result: model.AskResult{
			Answer: "Grounded answer",
			Citations: []model.Citation{
				{ChunkID: 12, RelPath: "docs/a.md", Span: model.Span{Kind: "lines", StartLine: 2, EndLine: 4}},
			},
		},
	}
	eng := &Engine{retriever: retriever}

	res, err := eng.Ask("What changed?", AskOptions{K: 3})
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if res.Answer != "Grounded answer" {
		t.Fatalf("unexpected answer: %q", res.Answer)
	}
	if len(res.Citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(res.Citations))
	}
	if res.Citations[0].RelPath != "docs/a.md" {
		t.Fatalf("unexpected citation rel_path: %q", res.Citations[0].RelPath)
	}
	if retriever.gotQuestion != "What changed?" {
		t.Fatalf("unexpected delegated question: %q", retriever.gotQuestion)
	}
	if retriever.gotQuery.K != 3 || retriever.gotQuery.Query != "What changed?" {
		t.Fatalf("unexpected delegated query: %#v", retriever.gotQuery)
	}
}

func TestEngineAsk_EmptyContext(t *testing.T) {
	retriever := &fakeEngineRetriever{
		result: model.AskResult{
			Answer:    "No relevant context found in the indexed corpus.",
			Citations: []model.Citation{},
		},
	}
	eng := &Engine{retriever: retriever}

	res, err := eng.Ask("unknown", AskOptions{K: 5})
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if res.Answer != "No relevant context found in the indexed corpus." {
		t.Fatalf("unexpected empty-context answer: %q", res.Answer)
	}
	if len(res.Citations) != 0 {
		t.Fatalf("expected no citations, got %#v", res.Citations)
	}
}

func TestEngineAsk_DefaultKWhenNonPositive(t *testing.T) {
	retriever := &fakeEngineRetriever{result: model.AskResult{Answer: "ok"}}
	eng := &Engine{retriever: retriever}

	if _, err := eng.Ask("q", AskOptions{K: 0}); err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if retriever.gotQuery.K != 10 {
		t.Fatalf("expected default k=10, got %d", retriever.gotQuery.K)
	}
}

func TestEngineAsk_ValidationAndErrors(t *testing.T) {
	eng := &Engine{}
	if _, err := eng.Ask("q", AskOptions{}); err == nil {
		t.Fatal("expected error when engine retriever is not configured")
	}

	eng = &Engine{retriever: &fakeEngineRetriever{}}
	if _, err := eng.Ask("   ", AskOptions{}); err == nil {
		t.Fatal("expected validation error for empty question")
	}

	wantErr := errors.New("boom")
	eng = &Engine{retriever: &fakeEngineRetriever{err: wantErr}}
	if _, err := eng.Ask("q", AskOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("expected delegated error %v, got %v", wantErr, err)
	}
}
