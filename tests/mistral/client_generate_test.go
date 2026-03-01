package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dir2mcp/internal/mistral"
	"dir2mcp/internal/model"
)

func TestGenerate_SuccessStringContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Hello from model"}},
			},
		})
	}))
	defer server.Close()

	client := mistral.NewClient(server.URL, "test-key")
	out, err := client.Generate(context.Background(), "Say hello")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if out != "Hello from model" {
		t.Fatalf("unexpected generated text: %q", out)
	}
}

func TestGenerate_SuccessArrayContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": []map[string]any{
							{"type": "text", "text": "first"},
							{"type": "text", "text": "second"},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := mistral.NewClient(server.URL, "test-key")
	out, err := client.Generate(context.Background(), "Two lines")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if out != "first\nsecond" {
		t.Fatalf("unexpected generated text: %q", out)
	}
}

func TestGenerate_RetriesOnRateLimit(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer server.Close()

	client := mistral.NewClient(server.URL, "test-key")
	client.MaxRetries = 2
	client.InitialBackoff = 1 * time.Millisecond
	client.MaxBackoff = 1 * time.Millisecond

	out, err := client.Generate(context.Background(), "retry me")
	if err != nil {
		t.Fatalf("Generate should have succeeded after retry: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected generated text: %q", out)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("unexpected call count: got %d want 2", got)
	}
}

func TestGenerate_MapsAuthErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid key"))
	}))
	defer server.Close()

	client := mistral.NewClient(server.URL, "bad-key")
	client.MaxRetries = 1

	_, err := client.Generate(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected auth error")
	}
	var providerErr *model.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "MISTRAL_AUTH" {
		t.Fatalf("unexpected code: %s", providerErr.Code)
	}
	if providerErr.Retryable {
		t.Fatal("auth errors should not be retryable")
	}
}

func TestGenerate_ValidatesPrompt(t *testing.T) {
	client := mistral.NewClient("https://api.mistral.ai", "test-key")
	_, err := client.Generate(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected prompt validation error")
	}
	var providerErr *model.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "MISTRAL_FAILED" {
		t.Fatalf("unexpected code: %s", providerErr.Code)
	}
	if !strings.Contains(providerErr.Message, "prompt is required") {
		t.Fatalf("unexpected message: %q", providerErr.Message)
	}
}
