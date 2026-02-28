package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{})
	sessionID := initializeSession(t, srv)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Session-Id", sessionID)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got: %#v", resp["result"])
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got: %#v", result["tools"])
	}
	for idx, toolVal := range tools {
		tool, ok := toolVal.(map[string]any)
		if !ok {
			t.Fatalf("expected tool object at index %d, got: %#v", idx, toolVal)
		}
		if _, ok := tool["name"].(string); !ok {
			t.Fatalf("expected tool.name string at index %d, got: %#v", idx, tool["name"])
		}
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}
}

func TestServer_ToolsCall_Search(t *testing.T) {
	hits := []model.SearchHit{
		{ChunkID: 7, RelPath: "docs/a.md", DocType: "md", Score: 0.9},
	}
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{hits: hits})
	sessionID := initializeSession(t, srv)
	reqBody := `{"jsonrpc":"2.0","id":"req-1","method":"tools/call","params":{"name":"dir2mcp.search","arguments":{"query":"hello","k":5}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Session-Id", sessionID)
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

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got: %#v", resp["result"])
	}
	content, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("expected content array, got: %#v", result["content"])
	}
	if len(content) == 0 {
		t.Fatal("expected at least one content item")
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got: %#v", result["structuredContent"])
	}
	hitsValue, ok := structured["hits"].([]any)
	if !ok {
		t.Fatalf("expected structuredContent.hits array, got: %#v", structured["hits"])
	}
	if len(hitsValue) != 1 {
		t.Fatalf("expected exactly 1 hit, got %d (%#v)", len(hitsValue), hitsValue)
	}

	// verify that the hit we returned matches the fixture we supplied above
	hitObj, ok := hitsValue[0].(map[string]any)
	if !ok {
		t.Fatalf("expected hit object, got %#v", hitsValue[0])
	}
	// chunk IDs are unmarshaled as float64 from JSON numbers
	if hitObj["ChunkID"] != float64(hits[0].ChunkID) {
		t.Fatalf("unexpected ChunkID: got %v, want %d", hitObj["ChunkID"], hits[0].ChunkID)
	}
	if hitObj["RelPath"] != hits[0].RelPath {
		t.Fatalf("unexpected RelPath: got %v, want %s", hitObj["RelPath"], hits[0].RelPath)
	}
	if hitObj["DocType"] != hits[0].DocType {
		t.Fatalf("unexpected DocType: got %v, want %s", hitObj["DocType"], hits[0].DocType)
	}
	// compare scores with a small epsilon to avoid float equality issues
	scoreVal, ok := hitObj["Score"].(float64)
	if !ok {
		t.Fatalf("expected hit score number, got %#v", hitObj["Score"])
	}
	if delta := math.Abs(scoreVal - hits[0].Score); delta > 1e-6 {
		t.Fatalf("unexpected Score: got %v, want %f (delta %g)", scoreVal, hits[0].Score, delta)
	}
	// ensure the k we sent is echoed back
	kVal, ok := structured["k"].(float64)
	if !ok {
		t.Fatalf("expected structuredContent.k number, got %#v", structured["k"])
	}
	if int(kVal) != 5 {
		t.Fatalf("expected structuredContent.k 5, got %v", kVal)
	}
}

func TestServer_ToolsCall_Search_DefaultK(t *testing.T) {
	hits := []model.SearchHit{{ChunkID: 42, RelPath: "foo.txt", DocType: "txt", Score: 1.0}}
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{hits: hits})
	sessionID := initializeSession(t, srv)
	// omit `k` entirely
	reqBody := `{"jsonrpc":"2.0","id":"req-2","method":"tools/call","params":{"name":"dir2mcp.search","arguments":{"query":"hello"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Session-Id", sessionID)
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

	resultMap, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got: %#v", resp["result"])
	}
	structured, ok := resultMap["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got: %#v", resultMap["structuredContent"])
	}
	kVal, ok := structured["k"].(float64)
	if !ok {
		t.Fatalf("expected structuredContent.k number, got %#v", structured["k"])
	}
	if int(kVal) != DefaultSearchK {
		t.Fatalf("expected default k %d, got %v", DefaultSearchK, kVal)
	}
}

func TestServer_ToolsCall_Search_NonPositiveK(t *testing.T) {
	hits := []model.SearchHit{{ChunkID: 99, RelPath: "bar.txt", DocType: "txt", Score: 0.5}}
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{hits: hits})
	sessionID := initializeSession(t, srv)

	for _, k := range []int{0, -1} {
		t.Run(fmt.Sprintf("k=%d", k), func(t *testing.T) {
			// send a non-positive k; should fall back to default
			reqBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":"req-3","method":"tools/call","params":{"name":"dir2mcp.search","arguments":{"query":"hello","k":%d}}}`, k)
			req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("MCP-Session-Id", sessionID)
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

			resultMap, ok := resp["result"].(map[string]any)
			if !ok {
				t.Fatalf("expected result object, got: %#v", resp["result"])
			}
			structured, ok := resultMap["structuredContent"].(map[string]any)
			if !ok {
				t.Fatalf("expected structuredContent object, got: %#v", resultMap["structuredContent"])
			}
			kVal, ok := structured["k"].(float64)
			if !ok {
				t.Fatalf("expected structuredContent.k number, got %#v", structured["k"])
			}
			if int(kVal) != DefaultSearchK {
				t.Fatalf("expected default k %d, got %v", DefaultSearchK, kVal)
			}
		})
	}
}

func TestServer_ToolsCall_MissingParams(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{})
	sessionID := initializeSession(t, srv)

	// omit params entirely
	reqBody := `{"jsonrpc":"2.0","id":"req-4","method":"tools/call"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Session-Id", sessionID)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)

	// missing parameters are considered a bad request by the server,
	// it returns 400 along with a JSON-RPC error object.  assert the
	// status code so we don't blindly decode an unexpected payload.
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got: %#v", resp["error"])
	}
	msg, _ := errObj["message"].(string)
	if msg != "params is required" {
		t.Fatalf("expected 'params is required' error, got %q", msg)
	}
	data, ok := errObj["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected error data object, got: %#v", errObj["data"])
	}
	if data["code"] != "MISSING_FIELD" {
		t.Fatalf("expected MISSING_FIELD code, got %v", data["code"])
	}
}

func initializeSession(t *testing.T, srv *Server) string {
	t.Helper()
	reqBody := `{"jsonrpc":"2.0","id":"init-1","method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("initialize failed status=%d body=%s", rr.Code, rr.Body.String())
	}
	sessionID := rr.Header().Get("MCP-Session-Id")
	if sessionID == "" {
		t.Fatal("missing MCP-Session-Id on initialize")
	}
	return sessionID
}
