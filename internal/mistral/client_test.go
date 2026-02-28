package mistral

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"dir2mcp/internal/model"
)

func TestExtract_OCRPagesJoinedByFormFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ocr" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("unexpected auth header: %q", got)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "mistral-ocr-latest" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}

		_, _ = w.Write([]byte(`{"pages":[{"markdown":"first page"},{"markdown":"second page"}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	got, err := c.Extract(context.Background(), "docs/file.pdf", []byte("pdf-bytes"))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if got != "first page\fsecond page" {
		t.Fatalf("unexpected ocr text: %q", got)
	}
}

func TestExtract_MapsUnauthorizedToProviderAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.Extract(context.Background(), "scan.png", []byte("img"))
	if err == nil {
		t.Fatalf("expected error")
	}
	var pe *model.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected provider error type")
	}
	if pe.Code != "MISTRAL_AUTH" {
		t.Fatalf("expected MISTRAL_AUTH, got %s", pe.Code)
	}
}

func TestExtract_MissingAPIKey(t *testing.T) {
	c := NewClient("http://example.test", "")
	_, err := c.Extract(context.Background(), "a.pdf", []byte("x"))
	if err == nil {
		t.Fatalf("expected error")
	}
	var pe *model.ProviderError
	if !errors.As(err, &pe) || pe.Code != "MISTRAL_AUTH" {
		t.Fatalf("expected provider auth error, got: %v", err)
	}
}
