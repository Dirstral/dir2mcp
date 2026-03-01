package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dir2mcp/internal/dirstral/mcp"
	"dir2mcp/internal/protocol"
	"dir2mcp/internal/x402"
)

func TestCanonicalCodeFromError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "equivalent phrase in message", err: errors.New("backend failure: session not found"), want: mcp.CanonicalCodeSessionNotFound},
		{name: "unauthorized alias", err: errors.New("request failed: unauthenticated"), want: mcp.CanonicalCodeUnauthorized},
		{name: "permission variant", err: errors.New("permission denied for this route"), want: mcp.CanonicalCodePermissionDenied},
		{name: "rate limit variant", err: errors.New("rate-limit exceeded; retry later"), want: mcp.CanonicalCodeRateLimited},
		{name: "contextual quota variant", err: errors.New("api quota exceeded for this request"), want: mcp.CanonicalCodeRateLimited},
		{name: "quota without request context", err: errors.New("disk quota reached for temp files"), want: ""},
		{name: "unknown", err: errors.New("something else"), want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mcp.CanonicalCodeFromError(tc.err); got != tc.want {
				t.Fatalf("CanonicalCodeFromError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestActionableMessageForCode(t *testing.T) {
	t.Parallel()

	msg := mcp.ActionableMessageForCode(mcp.CanonicalCodeIndexNotReady)
	if !strings.Contains(msg, "index is not ready") {
		t.Fatalf("unexpected index-not-ready message: %q", msg)
	}

	msg = mcp.ActionableMessageForCode(mcp.CanonicalCodeFileNotFound)
	if !strings.Contains(msg, "file") || !strings.Contains(msg, "not found") {
		t.Fatalf("unexpected file-not-found message: %q", msg)
	}

	if unknown := mcp.ActionableMessageForCode("DOES_NOT_EXIST"); unknown != "" {
		t.Fatalf("expected empty message for unknown code, got %q", unknown)
	}
}

func TestActionableMessageFromError(t *testing.T) {
	t.Parallel()

	err := errors.New("INDEX_NOT_READY")
	msg := mcp.ActionableMessageFromError(err)
	if !strings.Contains(strings.ToLower(msg), "index") {
		t.Fatalf("unexpected actionable message: %q", msg)
	}
}

func TestMCPPaymentRequiredActionableMessageFromHTTP402(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req["method"] {
		case "initialize":
			w.Header().Set(protocol.MCPSessionHeader, "session-1")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			w.Header().Set("payment-required", `{"x402Version":2}`)
			w.Header().Set("payment-response", `{"network":"base"}`)
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]any{"code": -32000, "message": "request blocked"},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := mcp.NewWithTransport(server.URL, "streamable-http", false)
	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	_, err := client.CallTool(ctx, protocol.ToolNameStats, map[string]any{})
	if err == nil {
		t.Fatal("expected payment-required error")
	}

	if got := mcp.CanonicalCodeFromError(err); got != x402.CodePaymentRequired {
		t.Fatalf("CanonicalCodeFromError() = %q, want %q", got, x402.CodePaymentRequired)
	}

	wantMessage := "This tool requires payment. Run with x402 enabled or configure a payment token."
	if got := mcp.ActionableMessageFromError(err); got != wantMessage {
		t.Fatalf("ActionableMessageFromError() = %q, want %q", got, wantMessage)
	}
}

func TestMCPPaymentRequiredActionableMessageWithHints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req["method"] {
		case "initialize":
			w.Header().Set(protocol.MCPSessionHeader, "session-1")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			w.Header().Set("payment-required", `{"x402Version":2, "accepts":[{"amount":"100", "asset":"USD", "network":"base", "payTo":"someone", "resource":"stats"}]}`)
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]any{"code": -32000, "message": "payment required"},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := mcp.NewWithTransport(server.URL, "streamable-http", false)
	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	_, err := client.CallTool(ctx, protocol.ToolNameStats, map[string]any{})
	if err == nil {
		t.Fatal("expected payment-required error")
	}

	wantMessage := "This tool requires payment. Run with x402 enabled or configure a payment token. (Hints: amount=100, asset=USD, network=base)"
	if got := mcp.ActionableMessageFromError(err); got != wantMessage {
		t.Fatalf("ActionableMessageFromError() = %q, want %q", got, wantMessage)
	}
}
