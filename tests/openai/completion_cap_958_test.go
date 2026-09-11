package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dirstral/dir2mcp/internal/openai"
)

// Issue #958, measured against the live API on 2026-09-11: every GPT-5-era model
// answers `400 Unsupported parameter: 'max_tokens' is not supported with this
// model. Use 'max_completion_tokens' instead.` The client always sent
// max_tokens, so dir2mcp could not drive ANY current OpenAI chat model, and the
// failure was quiet: retrieval catches the generator error and falls back to a
// raw context dump, so `ask` still answers 200 with citations.
//
// These tests pin the wire field, not an internal flag, because the wire is
// where the incompatibility lives.

// capRecorder is a fake /chat/completions that records which cap parameter each
// request carried, and can refuse one of them the way a real server does.
type capRecorder struct {
	mu       sync.Mutex
	sent     []string // "max_completion_tokens" or "max_tokens", per request
	values   []float64
	refuse   string // when set, a request carrying this key gets the 400 a real server sends
	refusals int
}

func (r *capRecorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		r.mu.Lock()
		key := ""
		for _, k := range []string{"max_completion_tokens", "max_tokens"} {
			if v, ok := body[k]; ok {
				if key != "" {
					// Sending both is a 400 on some servers, so it must never happen.
					t.Errorf("request carried BOTH cap parameters: %v", body)
				}
				key = k
				if f, ok := v.(float64); ok {
					r.values = append(r.values, f)
				}
			}
		}
		r.sent = append(r.sent, key)
		refuse := r.refuse
		if key == refuse {
			r.refusals++
		}
		r.mu.Unlock()

		if key == refuse && refuse != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: '` + refuse +
				`' is not supported with this model. Use the other one instead.","type":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"grounded answer"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (r *capRecorder) snapshot() ([]string, []float64, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...), append([]float64(nil), r.values...), r.refusals
}

// A modern endpoint must be driven with max_completion_tokens on the FIRST try:
// the parameter every current model requires, spent with no probe request.
func TestGenerate_SendsMaxCompletionTokens_958(t *testing.T) {
	rec := &capRecorder{}
	c := openai.NewClient(rec.server(t).URL+"/v1", "k")
	got, err := c.Generate(context.Background(), "why is the sky blue")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "grounded answer" {
		t.Fatalf("answer = %q", got)
	}
	sent, values, _ := rec.snapshot()
	if len(sent) != 1 || sent[0] != "max_completion_tokens" {
		t.Fatalf("wire parameters = %v, want exactly one max_completion_tokens: a GPT-5-era model "+
			"rejects max_tokens outright, so the modern name must be the first thing tried", sent)
	}
	if len(values) != 1 || values[0] <= 0 {
		t.Fatalf("cap value = %v, want a positive bound on every completion (#500)", values)
	}
}

// An OpenAI-COMPATIBLE server that predates the rename (older llama.cpp, vLLM,
// or any `kind: openai` profile) knows only max_tokens. It must keep working:
// one refusal, one retry under the legacy name, and a real answer.
func TestGenerate_FallsBackToMaxTokens_958(t *testing.T) {
	rec := &capRecorder{refuse: "max_completion_tokens"}
	c := openai.NewClient(rec.server(t).URL+"/v1", "k")
	got, err := c.Generate(context.Background(), "why is the sky blue")
	if err != nil {
		t.Fatalf("a server that knows only max_tokens must still be usable: %v", err)
	}
	if got != "grounded answer" {
		t.Fatalf("answer = %q", got)
	}
	sent, _, refusals := rec.snapshot()
	if len(sent) != 2 || sent[0] != "max_completion_tokens" || sent[1] != "max_tokens" {
		t.Fatalf("wire parameters = %v, want [max_completion_tokens max_tokens]", sent)
	}
	if refusals != 1 {
		t.Fatalf("refusals = %d, want 1", refusals)
	}

	// The fallback is sticky: the next call must not pay for the probe again.
	if _, err := c.Generate(context.Background(), "and again"); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	sent, _, refusals = rec.snapshot()
	if len(sent) != 3 || sent[2] != "max_tokens" {
		t.Fatalf("after the fallback the client must keep using max_tokens, got %v", sent)
	}
	if refusals != 1 {
		t.Fatalf("refusals = %d, want still 1: the refusal must be remembered, not re-probed", refusals)
	}
}

// A 400 about something ELSE is a real failure. It must surface, not trigger a
// second request under the other spelling.
func TestGenerate_OtherBadRequestDoesNotRetry_958(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model."}}`))
	}))
	t.Cleanup(srv.Close)
	c := openai.NewClient(srv.URL+"/v1", "k")
	_, err := c.Generate(context.Background(), "q")
	if err == nil {
		t.Fatal("a 400 naming another parameter must surface")
	}
	if !strings.Contains(err.Error(), "temperature") {
		t.Fatalf("error must name the real cause, got %v", err)
	}
	// The request count is the assertion that matters: retrying an unrelated 400
	// under the other spelling cannot help and doubles the cost of every bad
	// request. The error text alone does not catch that, because the retry
	// returns the same message.
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("server calls = %d, want 1: a 400 about another parameter must not be retried "+
			"under the other cap spelling", got)
	}
}

// A 5xx that happens to mention the parameter is an outage, not a rename. It is
// retryable by the normal backoff path, and it must NOT flip this client to the
// legacy spelling for the rest of its life: that would leave a modern endpoint
// permanently driven with a parameter every GPT-5-era model refuses, turning one
// transient blip into a dead generator.
func TestGenerate_ServerErrorDoesNotFlipTheCapName_958(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		shouldFail := fail
		fail = false
		mu.Unlock()
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"upstream is unsupported right now: max_completion_tokens"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"grounded answer"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := openai.NewClient(srv.URL+"/v1", "k")
	c.InitialBackoff = time.Millisecond
	c.MaxBackoff = time.Millisecond
	if _, err := c.Generate(context.Background(), "q"); err != nil {
		t.Fatalf("a 500 followed by a 200 must succeed on retry: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2 (one 500, one retry)", len(bodies))
	}
	for i, b := range bodies {
		if _, ok := b["max_completion_tokens"]; !ok {
			t.Fatalf("request %d used the legacy cap name after a 500; body = %v", i+1, b)
		}
	}
}
