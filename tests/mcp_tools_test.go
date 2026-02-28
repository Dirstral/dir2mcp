package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/mcp"
)

func TestMCPToolsList_RegistersDayOneToolsWithSchemas(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}

	var envelope struct {
		Result struct {
			Tools []struct {
				Name         string                 `json:"name"`
				InputSchema  map[string]interface{} `json:"inputSchema"`
				OutputSchema map[string]interface{} `json:"outputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	expected := map[string]bool{
		"dir2mcp.search":     false,
		"dir2mcp.ask":        false,
		"dir2mcp.open_file":  false,
		"dir2mcp.list_files": false,
		"dir2mcp.stats":      false,
	}

	for _, tool := range envelope.Result.Tools {
		if _, ok := expected[tool.Name]; !ok {
			continue
		}
		if len(tool.InputSchema) == 0 {
			t.Fatalf("tool %s missing inputSchema", tool.Name)
		}
		if len(tool.OutputSchema) == 0 {
			t.Fatalf("tool %s missing outputSchema", tool.Name)
		}
		expected[tool.Name] = true
	}

	for name, seen := range expected {
		if !seen {
			t.Fatalf("missing expected tool registration: %s", name)
		}
	}
}

func TestMCPToolsCallStats_ReturnsStructuredContent(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}

	var envelope struct {
		Result struct {
			IsError           bool                   `json:"isError"`
			StructuredContent map[string]interface{} `json:"structuredContent"`
			Content           []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if envelope.Result.IsError {
		t.Fatal("expected stats tool call to succeed")
	}
	if len(envelope.Result.Content) == 0 {
		t.Fatal("expected content item in tool response")
	}
	if envelope.Result.StructuredContent["protocol_version"] != cfg.ProtocolVersion {
		t.Fatalf("unexpected protocol_version: %#v", envelope.Result.StructuredContent["protocol_version"])
	}

	indexingRaw, ok := envelope.Result.StructuredContent["indexing"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected indexing object, got %#v", envelope.Result.StructuredContent["indexing"])
	}
	if _, ok := indexingRaw["mode"]; !ok {
		t.Fatalf("expected indexing.mode in response: %#v", indexingRaw)
	}

	modelsRaw, ok := envelope.Result.StructuredContent["models"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected models object, got %#v", envelope.Result.StructuredContent["models"])
	}
	if modelsRaw["stt_provider"] == "" {
		t.Fatalf("expected non-empty models.stt_provider, got %#v", modelsRaw["stt_provider"])
	}
}

func TestMCPToolsCallListFiles_GracefulWithoutSQLiteStore(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":10,"offset":0}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}

	var envelope struct {
		Result struct {
			IsError           bool                   `json:"isError"`
			StructuredContent map[string]interface{} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if envelope.Result.IsError {
		t.Fatal("expected list_files tool call to succeed")
	}
	if got := envelope.Result.StructuredContent["limit"]; got != float64(10) {
		t.Fatalf("unexpected limit: %#v", got)
	}
	if got := envelope.Result.StructuredContent["total"]; got != float64(0) {
		t.Fatalf("unexpected total: %#v", got)
	}

	filesRaw, ok := envelope.Result.StructuredContent["files"].([]interface{})
	if !ok {
		t.Fatalf("expected files array, got %#v", envelope.Result.StructuredContent["files"])
	}
	if len(filesRaw) != 0 {
		t.Fatalf("expected empty files list, got %#v", filesRaw)
	}
}

func initializeSession(t *testing.T, url string) string {
	t.Helper()
	resp := postRPC(t, url, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}
	sessionID := resp.Header.Get("MCP-Session-Id")
	if sessionID == "" {
		t.Fatal("missing MCP-Session-Id header")
	}
	return sessionID
}

func postRPC(t *testing.T, url, sessionID, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("MCP-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}
