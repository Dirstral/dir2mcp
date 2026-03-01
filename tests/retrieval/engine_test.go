package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/internal/retrieval"
)

func TestEngineAsk_WithEmptyIndexReturnsFallback(t *testing.T) {
	server := newFakeMistralEmbeddingServer()
	defer server.Close()

	stateDir := t.TempDir()
	rootDir := t.TempDir()

	cfg := config.Default()
	cfg.MistralAPIKey = "test-api-key"
	cfg.MistralBaseURL = server.URL

	engine, err := retrieval.NewEngine(context.Background(), stateDir, rootDir, &cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	t.Cleanup(engine.Close)

	result, err := engine.Ask("what changed?", retrieval.AskOptions{K: 3})
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil AskResult")
	}
	if !strings.Contains(result.Answer, "No relevant context") {
		t.Fatalf("expected empty-context fallback answer, got %q", result.Answer)
	}
	if len(result.Citations) != 0 {
		t.Fatalf("expected no citations for empty index, got %#v", result.Citations)
	}
}

func TestEngineAsk_RejectsEmptyQuestion(t *testing.T) {
	server := newFakeMistralEmbeddingServer()
	defer server.Close()

	stateDir := t.TempDir()
	rootDir := t.TempDir()

	cfg := config.Default()
	cfg.MistralAPIKey = "test-api-key"
	cfg.MistralBaseURL = server.URL

	engine, err := retrieval.NewEngine(context.Background(), stateDir, rootDir, &cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	t.Cleanup(engine.Close)

	_, err = engine.Ask("   ", retrieval.AskOptions{})
	if err == nil {
		t.Fatal("expected validation error for empty question")
	}
	if !strings.Contains(err.Error(), "question is required") {
		t.Fatalf("unexpected error for empty question: %v", err)
	}
}

func TestEngineAsk_ZeroValueEngineReportsNotInitialized(t *testing.T) {
	var engine retrieval.Engine
	_, err := engine.Ask("q", retrieval.AskOptions{})
	if err == nil {
		t.Fatal("expected error from zero-value engine")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("unexpected zero-value engine error: %v", err)
	}
}

func newFakeMistralEmbeddingServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		type item struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}

		data := make([]item, 0, len(req.Input))
		for i := range req.Input {
			data = append(data, item{Index: i, Embedding: []float64{1, 0}})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"data": data}); err != nil {
			// panic so the test fails fast if encoding unexpectedly fails
			panic(err)
		}
	}))
}
