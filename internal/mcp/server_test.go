package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

type fakeRetriever struct {
	hits              []model.SearchHit
	openFileContent   string
	openFileErr       error
	lastMaxChars      int // record value passed to OpenFile
	lastRelPath       string
	lastSpan          model.Span
	wasOpenFileCalled bool // true if OpenFile was invoked
}

func (f *fakeRetriever) Search(_ context.Context, _ model.SearchQuery) ([]model.SearchHit, error) {
	return f.hits, nil
}

func (f *fakeRetriever) Ask(_ context.Context, _ string, _ model.SearchQuery) (model.AskResult, error) {
	return model.AskResult{}, nil
}

func (f *fakeRetriever) OpenFile(_ context.Context, relPath string, span model.Span, maxChars int) (string, error) {
	f.wasOpenFileCalled = true
	f.lastRelPath = relPath
	f.lastSpan = span
	f.lastMaxChars = maxChars
	return f.openFileContent, f.openFileErr
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
	names := map[string]struct{}{}
	var searchTool map[string]any
	var openFileTool map[string]any
	for idx, toolVal := range tools {
		tool, ok := toolVal.(map[string]any)
		if !ok {
			t.Fatalf("expected tool object at index %d, got: %#v", idx, toolVal)
		}
		name, ok := tool["name"].(string)
		if !ok || name == "" {
			t.Fatalf("expected tool.name string at index %d, got: %#v", idx, tool["name"])
		}
		if _, exists := names[name]; exists {
			t.Fatalf("duplicate tool name in tools/list: %q", name)
		}
		names[name] = struct{}{}
		if name == "dir2mcp.search" {
			searchTool = tool
		}
		if name == "dir2mcp.open_file" {
			openFileTool = tool
		}
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}
	if _, ok := names["dir2mcp.search"]; !ok {
		t.Fatalf("expected dir2mcp.search in tools/list")
	}
	if searchTool == nil {
		t.Fatalf("expected to capture dir2mcp.search tool payload")
	}

	// validate search schema
	inputSchema, ok := searchTool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("expected dir2mcp.search.inputSchema object, got %#v", searchTool["inputSchema"])
	}
	if schemaType, _ := inputSchema["type"].(string); schemaType != "object" {
		t.Fatalf("expected dir2mcp.search.inputSchema.type=object, got %#v", inputSchema["type"])
	}
	required, ok := inputSchema["required"].([]any)
	if !ok {
		t.Fatalf("expected dir2mcp.search.inputSchema.required array, got %#v", inputSchema["required"])
	}
	foundQueryRequired := false
	for _, item := range required {
		if v, ok := item.(string); ok && v == "query" {
			foundQueryRequired = true
			break
		}
	}
	if !foundQueryRequired {
		t.Fatalf("expected dir2mcp.search.inputSchema.required to include query, got %#v", required)
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected dir2mcp.search.inputSchema.properties object, got %#v", inputSchema["properties"])
	}
	for _, key := range []string{"query", "k", "index", "path_prefix", "file_glob", "doc_types"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("expected dir2mcp.search.inputSchema.properties to include %q", key)
		}
	}

	// ensure open_file tool exists and has sensible schema
	if _, ok := names["dir2mcp.open_file"]; !ok {
		t.Fatalf("expected dir2mcp.open_file in tools/list")
	}
	if openFileTool == nil {
		t.Fatalf("expected to capture dir2mcp.open_file tool payload")
	}
	openSchema, ok := openFileTool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("expected dir2mcp.open_file.inputSchema object, got %#v", openFileTool["inputSchema"])
	}
	if schemaType, _ := openSchema["type"].(string); schemaType != "object" {
		t.Fatalf("expected dir2mcp.open_file.inputSchema.type=object, got %#v", openSchema["type"])
	}
	required, ok = openSchema["required"].([]any)
	if !ok {
		t.Fatalf("expected dir2mcp.open_file.inputSchema.required array, got %#v", openSchema["required"])
	}
	foundRelPath := false
	for _, item := range required {
		if v, ok := item.(string); ok && v == "rel_path" {
			foundRelPath = true
			break
		}
	}
	if !foundRelPath {
		t.Fatalf("expected dir2mcp.open_file.inputSchema.required to include rel_path, got %#v", required)
	}
	properties, ok = openSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected dir2mcp.open_file.inputSchema.properties object, got %#v", openSchema["properties"])
	}
	for _, key := range []string{"rel_path", "start_line", "end_line", "max_chars"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("expected dir2mcp.open_file.inputSchema.properties to include %q", key)
		}
	}
	// ensure the schema enforces positive line numbers
	if startProp, ok := properties["start_line"].(map[string]any); ok {
		if m, ok := startProp["minimum"].(float64); !ok || m < 1 {
			t.Fatalf("start_line.minimum should be >=1, got %#v", startProp["minimum"])
		}
	} else {
		t.Fatalf("start_line property missing from schema")
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

func TestServer_ToolsCall_OpenFile(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	fr := &fakeRetriever{openFileContent: "line two"}
	srv := NewServer(cfg, fr)
	sessionID := initializeSession(t, srv)
	reqBody := `{"jsonrpc":"2.0","id":"open-1","method":"tools/call","params":{"name":"dir2mcp.open_file","arguments":{"rel_path":"docs/a.md","start_line":2,"end_line":2,"max_chars":200}}}`
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
	// legacy content field for backward compatibility
	contentArr, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("expected content array, got: %#v", result["content"])
	}
	if len(contentArr) != 1 {
		t.Fatalf("expected 1 legacy content item, got %d (%#v)", len(contentArr), contentArr)
	}
	itemMap, ok := contentArr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected legacy content item to be object, got %#v", contentArr[0])
	}
	if itemMap["text"] != "line two" || itemMap["type"] != "text" {
		t.Fatalf("unexpected legacy content item fields: %#v", itemMap)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got: %#v", result["structuredContent"])
	}
	if structured["rel_path"] != "docs/a.md" {
		t.Fatalf("unexpected rel_path: %v", structured["rel_path"])
	}
	if structured["doc_type"] != "md" {
		t.Fatalf("unexpected doc_type: %v", structured["doc_type"])
	}
	if structured["content"] != "line two" {
		t.Fatalf("unexpected content: %v", structured["content"])
	}
	if fr.lastRelPath != "docs/a.md" {
		t.Fatalf("unexpected rel_path forwarded to retriever: %q", fr.lastRelPath)
	}
	if fr.lastMaxChars != 200 {
		t.Fatalf("unexpected max_chars forwarded to retriever: got %d want %d", fr.lastMaxChars, 200)
	}
	if fr.lastSpan.Kind != "lines" || fr.lastSpan.StartLine != 2 || fr.lastSpan.EndLine != 2 {
		t.Fatalf("unexpected span forwarded to retriever: %#v", fr.lastSpan)
	}
}

func TestServer_ToolsCall_OpenFile_InvalidLines(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	cases := []struct {
		args    map[string]interface{}
		message string
	}{
		{map[string]interface{}{"rel_path": "docs/a.md", "start_line": 0}, "start_line"},
		{map[string]interface{}{"rel_path": "docs/a.md", "end_line": 0}, "end_line"},
		// negative values should also be rejected
		{map[string]interface{}{"rel_path": "docs/a.md", "start_line": -1}, "start_line"},
		{map[string]interface{}{"rel_path": "docs/a.md", "end_line": -1}, "end_line"},
	}

	for _, c := range cases {
		// create fresh retriever/server so wasOpenFileCalled starts false
		fr := &fakeRetriever{}
		srv := NewServer(cfg, fr)
		sessionID := initializeSession(t, srv)
		// build the JSON body using Go structs to avoid quoting errors
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "bad",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name":      "dir2mcp.open_file",
				"arguments": c.args,
			},
		}
		reqBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(reqBytes))
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
		// tool-level errors are returned inside the result payload
		resultObj, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result object, got %#v", resp["result"])
		}
		isError, _ := resultObj["isError"].(bool)
		if !isError {
			t.Fatalf("expected tool error for %v; response body=%s", c.args, rr.Body.String())
		}
		structured, ok := resultObj["structuredContent"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent object, got %#v", resultObj["structuredContent"])
		}
		errEnvelope, ok := structured["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent.error object, got %#v", structured["error"])
		}
		if errEnvelope["code"] != "INVALID_FIELD" {
			t.Fatalf("expected INVALID_FIELD code, got %v", errEnvelope["code"])
		}
		msg, _ := errEnvelope["message"].(string)
		if !strings.Contains(msg, c.message) {
			t.Fatalf("unexpected error message: %v", msg)
		}
		if fr.wasOpenFileCalled {
			t.Fatalf("retriever should not have been called for invalid args %v", c.args)
		}
	}
}

func TestServer_ToolsCall_OpenFile_ConflictingSpanParameters(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{})
	sessionID := initializeSession(t, srv)

	cases := []struct {
		args    map[string]interface{}
		message string
	}{
		{map[string]interface{}{"rel_path": "docs/a.md", "page": 2, "start_ms": 100}, "conflicting span parameters"},
		{map[string]interface{}{"rel_path": "docs/a.md", "page": 2, "start_line": 1}, "conflicting span parameters"},
		{map[string]interface{}{"rel_path": "docs/a.md", "start_ms": 0, "start_line": 1}, "conflicting span parameters"},
		{map[string]interface{}{"rel_path": "docs/a.md", "start_ms": 0, "end_ms": 10, "start_line": 1}, "conflicting span parameters"},
	}

	for _, c := range cases {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "bad",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name":      "dir2mcp.open_file",
				"arguments": c.args,
			},
		}
		reqBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(reqBytes))
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
		resultObj, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result object, got %#v", resp["result"])
		}
		isError, _ := resultObj["isError"].(bool)
		if !isError {
			t.Fatalf("expected tool error for %v; response body=%s", c.args, rr.Body.String())
		}
		structured, ok := resultObj["structuredContent"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent object, got %#v", resultObj["structuredContent"])
		}
		errEnvelope, ok := structured["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent.error object, got %#v", structured["error"])
		}
		if errEnvelope["code"] != "INVALID_FIELD" {
			t.Fatalf("expected INVALID_FIELD code, got %v", errEnvelope["code"])
		}
		msg, _ := errEnvelope["message"].(string)
		if !strings.Contains(msg, c.message) {
			t.Fatalf("unexpected error message: %v", msg)
		}
	}
}

func TestServer_ToolsCall_OpenFile_PartialSpanParameters(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{})
	sessionID := initializeSession(t, srv)

	cases := []struct {
		args    map[string]interface{}
		message string
	}{
		{map[string]interface{}{"rel_path": "docs/a.md", "start_ms": 1000}, "start_ms"},
		{map[string]interface{}{"rel_path": "docs/a.md", "end_ms": 2000}, "end_ms"},
		{map[string]interface{}{"rel_path": "docs/a.md", "start_line": 1}, "start_line"},
		{map[string]interface{}{"rel_path": "docs/a.md", "end_line": 2}, "end_line"},
	}

	for _, c := range cases {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "bad",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name":      "dir2mcp.open_file",
				"arguments": c.args,
			},
		}
		reqBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(reqBytes))
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
		resultObj, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("expected result object, got %#v", resp["result"])
		}
		isError, _ := resultObj["isError"].(bool)
		if !isError {
			t.Fatalf("expected tool error for %v; response body=%s", c.args, rr.Body.String())
		}
		structured, ok := resultObj["structuredContent"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent object, got %#v", resultObj["structuredContent"])
		}
		errEnvelope, ok := structured["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected structuredContent.error object, got %#v", structured["error"])
		}
		msg, _ := errEnvelope["message"].(string)
		if !strings.Contains(msg, c.message) {
			t.Fatalf("unexpected error message: %v", msg)
		}
	}
}

func Test_inferDocType_VariousExtensions(t *testing.T) {
	cases := map[string]string{
		"file.jsx":   "code",
		"module.tsx": "code",
		"script.sh":  "code",
		"style.css":  "html",
		"index.html": "html",
		"notes.txt":  "text",
		"image.png":  "image",
		"doc.md":     "md",
	}
	for path, want := range cases {
		if got := inferDocType(path); got != want {
			t.Errorf("inferDocType(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestServer_ToolsCall_OpenFile_EnforceMinChars(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	fr := &fakeRetriever{openFileContent: "hello", wasOpenFileCalled: false}
	srv := NewServer(cfg, fr)
	sessionID := initializeSession(t, srv)
	// send a request with a too-small max_chars; handler should reject it
	reqBody := `{"jsonrpc":"2.0","id":"open-min","method":"tools/call","params":{"name":"dir2mcp.open_file","arguments":{"rel_path":"docs/a.md","max_chars":10}}}`
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
	resultObj, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", resp["result"])
	}
	isError, _ := resultObj["isError"].(bool)
	if !isError {
		t.Fatalf("expected tool error response, got %#v", resultObj)
	}
	structured, ok := resultObj["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got %#v", resultObj["structuredContent"])
	}
	errEnvelope, ok := structured["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent.error object, got %#v", structured["error"])
	}
	if errEnvelope["code"] != "INVALID_FIELD" {
		t.Fatalf("expected INVALID_FIELD code, got %v", errEnvelope["code"])
	}
	if fr.wasOpenFileCalled {
		t.Fatalf("retriever should not be called on invalid args")
	}
	// lastMaxChars remains available for future value checks if needed
}

func TestServer_ToolsCall_OpenFile_PageSpanForwarded(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	fr := &fakeRetriever{openFileContent: "page two"}
	srv := NewServer(cfg, fr)
	sessionID := initializeSession(t, srv)
	reqBody := `{"jsonrpc":"2.0","id":"open-page","method":"tools/call","params":{"name":"dir2mcp.open_file","arguments":{"rel_path":"docs/a.pdf","page":2,"max_chars":200}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Session-Id", sessionID)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if fr.lastSpan.Kind != "page" || fr.lastSpan.Page != 2 {
		t.Fatalf("unexpected page span forwarded: %#v", fr.lastSpan)
	}
}

func TestServer_ToolsCall_OpenFile_TimeSpanForwarded(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	fr := &fakeRetriever{openFileContent: "clip"}
	srv := NewServer(cfg, fr)
	sessionID := initializeSession(t, srv)
	reqBody := `{"jsonrpc":"2.0","id":"open-time","method":"tools/call","params":{"name":"dir2mcp.open_file","arguments":{"rel_path":"audio/t.txt","start_ms":1000,"end_ms":2000,"max_chars":200}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Session-Id", sessionID)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if fr.lastSpan.Kind != "time" || fr.lastSpan.StartMS != 1000 || fr.lastSpan.EndMS != 2000 {
		t.Fatalf("unexpected time span forwarded: %#v", fr.lastSpan)
	}
}

func TestServer_ToolsCall_OpenFile_Forbidden(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	srv := NewServer(cfg, &fakeRetriever{openFileErr: model.ErrForbidden})
	sessionID := initializeSession(t, srv)
	reqBody := `{"jsonrpc":"2.0","id":"open-2","method":"tools/call","params":{"name":"dir2mcp.open_file","arguments":{"rel_path":"private/secret.txt"}}}`
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
	resultObj, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %#v", resp["result"])
	}
	isError, _ := resultObj["isError"].(bool)
	if !isError {
		t.Fatalf("expected tool error response, got %#v", resultObj)
	}
	structured, ok := resultObj["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got %#v", resultObj["structuredContent"])
	}
	errEnvelope, ok := structured["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent.error object, got %#v", structured["error"])
	}
	if errEnvelope["code"] != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN code, got %v", errEnvelope["code"])
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
