package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"dir2mcp/internal/dirstral/mcp"
	"dir2mcp/internal/protocol"
)

func reportHandlerErr(ch chan error, format string, args ...any) {
	select {
	case ch <- fmt.Errorf(format, args...):
	default:
	}
}

func assertNoHandlerErr(t *testing.T, ch chan error) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("handler assertion failed: %v", err)
	default:
	}
}

func TestCallToolRecoversSessionOnceOnSessionNotFound(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	initializeCalls := 0
	notifyCalls := 0
	toolCalls := 0
	handlerErrCh := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			reportHandlerErr(handlerErrCh, "decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		method, _ := req["method"].(string)

		mu.Lock()
		defer mu.Unlock()

		switch method {
		case "initialize":
			initializeCalls++
			session := "session-1"
			if initializeCalls > 1 {
				session = "session-2"
			}
			w.Header().Set(protocol.MCPSessionHeader, session)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "notifications/initialized":
			notifyCalls++
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			toolCalls++
			sessionHeader := r.Header.Get(protocol.MCPSessionHeader)
			if toolCalls == 1 {
				if sessionHeader != "session-1" {
					reportHandlerErr(handlerErrCh, "expected first tool call with session-1, got %q", sessionHeader)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"error":   map[string]any{"code": -32001, "message": protocol.ErrorCodeSessionNotFound},
				})
				return
			}
			if sessionHeader != "session-2" {
				reportHandlerErr(handlerErrCh, "expected retried tool call with session-2, got %q", sessionHeader)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
			})
		default:
			reportHandlerErr(handlerErrCh, "unexpected method: %s", method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}))
	defer server.Close()

	client := mcp.NewWithTransport(server.URL, "streamable-http", false)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initial initialize failed: %v", err)
	}

	res, err := client.CallTool(ctx, protocol.ToolNameSearch, map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "ok" {
		t.Fatalf("unexpected tool result: %#v", res.Content)
	}

	mu.Lock()
	defer mu.Unlock()
	if initializeCalls != 2 {
		t.Fatalf("expected 2 initialize calls, got %d", initializeCalls)
	}
	if notifyCalls != 2 {
		t.Fatalf("expected 2 notifications/initialized calls, got %d", notifyCalls)
	}
	if toolCalls != 2 {
		t.Fatalf("expected 2 tools/call attempts, got %d", toolCalls)
	}
	assertNoHandlerErr(t, handlerErrCh)
}

func TestCallToolSessionRecoveryIsBoundedToSingleRetry(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	initializeCalls := 0
	toolCalls := 0
	handlerErrCh := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			reportHandlerErr(handlerErrCh, "decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		method, _ := req["method"].(string)

		mu.Lock()
		defer mu.Unlock()

		switch method {
		case "initialize":
			initializeCalls++
			w.Header().Set(protocol.MCPSessionHeader, "session")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			toolCalls++
			if toolCalls > 2 {
				reportHandlerErr(handlerErrCh, "unexpected extra retry, tools/call=%d", toolCalls)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]any{"code": -32001, "message": "session not found"},
			})
		default:
			reportHandlerErr(handlerErrCh, "unexpected method: %s", method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}))
	defer server.Close()

	client := mcp.NewWithTransport(server.URL, "streamable-http", false)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initial initialize failed: %v", err)
	}

	_, err := client.CallTool(ctx, protocol.ToolNameSearch, map[string]any{"query": "hello"})
	if err == nil {
		t.Fatal("expected error after bounded retry")
	}

	mu.Lock()
	defer mu.Unlock()
	if initializeCalls != 2 {
		t.Fatalf("expected 2 initialize calls (initial + recovery), got %d", initializeCalls)
	}
	if toolCalls != 2 {
		t.Fatalf("expected exactly 2 tools/call attempts, got %d", toolCalls)
	}
	assertNoHandlerErr(t, handlerErrCh)
}
