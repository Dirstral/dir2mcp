package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/model"
)

type fakeRetriever struct {
	hits []model.SearchHit
}

func (f *fakeRetriever) Search(_ context.Context, _ model.SearchQuery) ([]model.SearchHit, error) {
	return f.hits, nil
}

func (f *fakeRetriever) Ask(_ context.Context, _ string, _ model.SearchQuery) (model.AskResult, error) {
	return model.AskResult{}, nil
}

func (f *fakeRetriever) OpenFile(_ context.Context, _ string, _ model.Span, _ int) (string, error) {
	return "", nil
}

func (f *fakeRetriever) Stats(_ context.Context) (model.Stats, error) {
	return model.Stats{}, nil
}

func TestServer_ToolsList(t *testing.T) {
	srv := NewServer(config.Default(), &fakeRetriever{})
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}
}

func TestServer_ToolsCall_Search(t *testing.T) {
	hits := []model.SearchHit{
		{ChunkID: 7, RelPath: "docs/a.md", DocType: "md", Score: 0.9},
	}
	srv := NewServer(config.Default(), &fakeRetriever{hits: hits})
	reqBody := `{"jsonrpc":"2.0","id":"req-1","method":"tools/call","params":{"name":"dir2mcp.search","arguments":{"query":"hello","k":5}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["error"] != nil {
		t.Fatalf("expected no error, got %v", resp["error"])
	}
}
