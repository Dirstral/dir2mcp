package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/internal/mcp"
	"dir2mcp/internal/model"
)

// TestMCPToolsList_RegistersDayOneToolsWithSchemas verifies that tools/list
// advertises the expected Day 1 MCP tools with schemas.
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
		"dir2mcp.ask_audio":  false,
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

func TestMCPToolsCallAskAudio_NilRetrieverReturnsIndexNotReady(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"dir2mcp.ask_audio","arguments":{"question":"What is indexed?"}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "INDEX_NOT_READY")
}

func TestMCPToolsCallAskAudio_AskNotImplementedReturnsGracefulSuccess(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	retriever := &askAudioRetrieverStub{
		askErr: model.ErrNotImplemented,
	}
	server := httptest.NewServer(mcp.NewServer(cfg, retriever).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"dir2mcp.ask_audio","arguments":{"question":"What is indexed?"}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}

	var envelope struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if envelope.Result.IsError {
		t.Fatal("expected graceful success for not-implemented ask")
	}
	if len(envelope.Result.Content) == 0 {
		t.Fatal("expected at least one content item")
	}
	if !strings.Contains(strings.ToLower(envelope.Result.Content[0].Text), "dir2mcp.search") {
		t.Fatalf("expected fallback guidance to dir2mcp.search, got %q", envelope.Result.Content[0].Text)
	}
}

func TestMCPToolsCallAskAudio_WithoutTTSReturnsTextOnly(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	retriever := &askAudioRetrieverStub{
		askResult: model.AskResult{
			Question:         "What is indexed?",
			Answer:           "Indexed content is available.",
			Citations:        []model.Citation{},
			Hits:             []model.SearchHit{},
			IndexingComplete: true,
		},
	}
	server := httptest.NewServer(mcp.NewServer(cfg, retriever).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"dir2mcp.ask_audio","arguments":{"question":"What is indexed?"}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}

	var envelope struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if envelope.Result.IsError {
		t.Fatal("expected non-error response")
	}
	if len(envelope.Result.Content) != 1 {
		t.Fatalf("expected one text content item, got %#v", envelope.Result.Content)
	}
	if envelope.Result.Content[0].Type != "text" {
		t.Fatalf("expected text content item, got %#v", envelope.Result.Content[0])
	}
	if !strings.Contains(envelope.Result.Content[0].Text, "ELEVENLABS_API_KEY") {
		t.Fatalf("expected configuration hint for ELEVENLABS_API_KEY, got %q", envelope.Result.Content[0].Text)
	}
}

func TestMCPToolsCallAskAudio_WithTTSReturnsTextAndAudio(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	retriever := &askAudioRetrieverStub{
		askResult: model.AskResult{
			Question:         "What is indexed?",
			Answer:           "Indexed content is available.",
			Citations:        []model.Citation{},
			Hits:             []model.SearchHit{},
			IndexingComplete: true,
		},
	}
	tts := &fakeTTSSynthesizer{
		audio: []byte("fake-mp3-bytes"),
	}
	server := httptest.NewServer(mcp.NewServer(cfg, retriever, mcp.WithTTS(tts)).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"dir2mcp.ask_audio","arguments":{"question":"What is indexed?"}}}`)
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
			Content           []toolContentEnvelope  `json:"content"`
			StructuredContent map[string]interface{} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if envelope.Result.IsError {
		t.Fatal("expected successful ask_audio response")
	}
	if len(envelope.Result.Content) != 2 {
		t.Fatalf("expected text + audio content items, got %#v", envelope.Result.Content)
	}

	textItem := envelope.Result.Content[0]
	audioItem := envelope.Result.Content[1]
	if textItem.Type != "text" {
		t.Fatalf("unexpected text item: %#v", textItem)
	}
	if audioItem.Type != "audio" {
		t.Fatalf("unexpected audio item type: %#v", audioItem)
	}
	if audioItem.MIMEType != "audio/mpeg" {
		t.Fatalf("unexpected mime type: %q", audioItem.MIMEType)
	}

	wantEncoded := base64.StdEncoding.EncodeToString([]byte("fake-mp3-bytes"))
	if audioItem.Data != wantEncoded {
		t.Fatalf("unexpected audio data payload: got=%q want=%q", audioItem.Data, wantEncoded)
	}

	audioRaw, ok := envelope.Result.StructuredContent["audio"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected structuredContent.audio object, got %#v", envelope.Result.StructuredContent["audio"])
	}
	if gotMime, _ := audioRaw["mime_type"].(string); gotMime != "audio/mpeg" {
		t.Fatalf("unexpected structured audio mime_type: %#v", audioRaw["mime_type"])
	}
	if gotData, _ := audioRaw["data"].(string); gotData != wantEncoded {
		t.Fatalf("unexpected structured audio data: %#v", audioRaw["data"])
	}
}

// TestMCPToolsCallStats_ReturnsStructuredContent verifies the happy-path
// response shape for dir2mcp.stats.
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
	sttProvider, ok := modelsRaw["stt_provider"].(string)
	if !ok || sttProvider == "" {
		t.Fatalf("expected non-empty string models.stt_provider, got %#v", modelsRaw["stt_provider"])
	}
}

// TestMCPToolsCallListFiles_GracefulWithoutSQLiteStore verifies that
// list_files returns an empty, valid response when no store is configured.
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

// TestMCPToolsCallStats_RejectsUnknownArgument verifies stats argument
// validation failures are reported as INVALID_FIELD.
func TestMCPToolsCallStats_RejectsUnknownArgument(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{"unexpected":true}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "INVALID_FIELD")
}

// TestMCPToolsCallListFiles_RejectsUnknownArgument verifies unknown
// list_files arguments are rejected.
func TestMCPToolsCallListFiles_RejectsUnknownArgument(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":10,"offset":0,"foo":"bar"}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "INVALID_FIELD")
}

// TestMCPToolsCallListFiles_RejectsLimitWrongType verifies non-integer limit
// values are rejected with INVALID_FIELD.
func TestMCPToolsCallListFiles_RejectsLimitWrongType(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":"10","offset":0}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "INVALID_FIELD")
}

// TestMCPToolsCallListFiles_RejectsLimitOutOfRange verifies list_files limit
// range checks (min and max bounds).
func TestMCPToolsCallListFiles_RejectsLimitOutOfRange(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{
			name: "limit_zero",
			body: `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":0,"offset":0}}}`,
			code: "INVALID_RANGE",
		},
		{
			name: "limit_too_large",
			body: `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":5001,"offset":0}}}`,
			code: "INVALID_RANGE",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, tc.body)
			defer func() {
				_ = resp.Body.Close()
			}()
			assertToolCallErrorCode(t, resp, tc.code)
		})
	}
}

// TestMCPToolsCallListFiles_RejectsOffsetWrongType verifies non-integer offset
// values are rejected with INVALID_FIELD.
func TestMCPToolsCallListFiles_RejectsOffsetWrongType(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":10,"offset":"0"}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "INVALID_FIELD")
}

// TestMCPToolsCallListFiles_RejectsNegativeOffset verifies negative offsets
// are rejected with INVALID_RANGE.
func TestMCPToolsCallListFiles_RejectsNegativeOffset(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":10,"offset":-1}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "INVALID_RANGE")
}

// TestMCPToolsCallListFiles_StoreFailureReturnsStoreCorrupt verifies store
// backend failures are surfaced as STORE_CORRUPT tool errors.
func TestMCPToolsCallListFiles_StoreFailureReturnsStoreCorrupt(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(
		mcp.NewServer(cfg, nil, mcp.WithStore(&failingListFilesStore{err: errors.New("boom")})).Handler(),
	)
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)

	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"dir2mcp.list_files","arguments":{"limit":10,"offset":0}}}`)
	defer func() {
		_ = resp.Body.Close()
	}()

	assertToolCallErrorCode(t, resp, "STORE_CORRUPT")
}

// failingListFilesStore is a minimal store stub that forces ListFiles to
// return a configured error for error-path testing.
type failingListFilesStore struct {
	err error
}

func (s *failingListFilesStore) Init(_ context.Context) error {
	return nil
}

func (s *failingListFilesStore) UpsertDocument(_ context.Context, _ model.Document) error {
	return nil
}

func (s *failingListFilesStore) GetDocumentByPath(_ context.Context, _ string) (model.Document, error) {
	return model.Document{}, model.ErrNotImplemented
}

func (s *failingListFilesStore) ListFiles(_ context.Context, _, _ string, _, _ int) ([]model.Document, int64, error) {
	return nil, 0, s.err
}

func (s *failingListFilesStore) Close() error {
	return nil
}

// assertToolCallErrorCode validates that a tools/call response returned a
// tool-level error payload with the expected canonical error code.
func assertToolCallErrorCode(t *testing.T, resp *http.Response, wantCode string) {
	t.Helper()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}

	var envelope struct {
		Result struct {
			IsError           bool                   `json:"isError"`
			StructuredContent map[string]interface{} `json:"structuredContent"`
		} `json:"result"`
		Error interface{} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if envelope.Error != nil {
		t.Fatalf("expected tool-level error result, got top-level error: %#v", envelope.Error)
	}
	if !envelope.Result.IsError {
		t.Fatalf("expected isError=true, got false with structuredContent=%#v", envelope.Result.StructuredContent)
	}

	errObjRaw, ok := envelope.Result.StructuredContent["error"]
	if !ok {
		t.Fatalf("expected structuredContent.error, got %#v", envelope.Result.StructuredContent)
	}
	errObj, ok := errObjRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected structuredContent.error object, got %#v", errObjRaw)
	}
	gotCode, ok := errObj["code"].(string)
	if !ok {
		t.Fatalf("expected structuredContent.error.code string, got %#v", errObj["code"])
	}
	if gotCode != wantCode {
		t.Fatalf("unexpected error code: got=%q want=%q full_error=%#v", gotCode, wantCode, errObj)
	}
}

// initializeSession performs MCP initialize and returns the session id used
// for subsequent tools/list and tools/call requests.
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

// postRPC sends a JSON-RPC POST request to the MCP endpoint with an optional
// MCP session header.
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

type askAudioRetrieverStub struct {
	askResult model.AskResult
	askErr    error
}

func (s *askAudioRetrieverStub) Search(_ context.Context, _ model.SearchQuery) ([]model.SearchHit, error) {
	return nil, model.ErrNotImplemented
}

func (s *askAudioRetrieverStub) Ask(_ context.Context, _ string, _ model.SearchQuery) (model.AskResult, error) {
	if s.askErr != nil {
		return model.AskResult{}, s.askErr
	}
	return s.askResult, nil
}

func (s *askAudioRetrieverStub) OpenFile(_ context.Context, _ string, _ model.Span, _ int) (string, error) {
	return "", model.ErrNotImplemented
}

func (s *askAudioRetrieverStub) Stats(_ context.Context) (model.Stats, error) {
	return model.Stats{}, model.ErrNotImplemented
}

type fakeTTSSynthesizer struct {
	audio []byte
	err   error
}

func (f *fakeTTSSynthesizer) Synthesize(_ context.Context, _ string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.audio, nil
}

type toolContentEnvelope struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}
