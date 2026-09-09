package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/tei"
)

// fakeTEI is an httptest stand-in for a Text Embeddings Inference server on its
// native surface, faithful to the facts the client relies on (verified against a
// live TEI 1.9.x): /tokenize reports BYTE offsets and null offsets for the
// [CLS]/[SEP] specials it adds; /embed_all returns one raw vector per token
// position, specials included; /embed returns pooled vectors; /info reports the
// served pooling and max_input_length. Its tokenizer splits on ASCII spaces so
// tests can predict every offset.
type fakeTEI struct {
	pooling        string
	maxInputLength int
	dim            int

	mu           sync.Mutex
	tokenizeReqs []map[string]any
	embedAllReqs []map[string]any
	embedReqs    []map[string]any
	authHeaders  []string

	// embedAllStatus, when non-zero, makes /embed_all answer that status with a
	// TEI-shaped error body instead of vectors.
	embedAllStatus int
	// embedAllDropLast, when set, drops the last token vector from /embed_all so
	// the response no longer lines up with /tokenize.
	embedAllDropLast bool
	// embedStatus, when non-zero, makes /embed answer that status.
	embedStatus int
	embedCalls  atomic.Int32
}

type fakeToken struct {
	text       string
	start, end int
}

// spaceTokens splits s on single ASCII spaces, reporting byte offsets.
func spaceTokens(s string) []fakeToken {
	var out []fakeToken
	start := -1
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' {
			if start >= 0 {
				out = append(out, fakeToken{text: s[start:i], start: start, end: i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	return out
}

// tokenVector is the deterministic raw vector for the i-th content token of a
// text: [i+1, 1, 0...], distinct per position so a pooled mean is checkable.
func tokenVector(i, dim int) []float64 {
	v := make([]float64, dim)
	v[0] = float64(i + 1)
	v[1] = 1
	return v
}

func (f *fakeTEI) record(dst *[]map[string]any, r *http.Request) map[string]any {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	*dst = append(*dst, body)
	f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
	f.mu.Unlock()
	return body
}

func (f *fakeTEI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model_id":          "fake/mini",
			"max_input_length":  f.maxInputLength,
			"model_type":        map[string]any{"embedding": map[string]any{"pooling": f.pooling}},
			"auto_truncate":     true,
			"served_model_name": "fake/mini",
		})
	})
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		body := f.record(&f.tokenizeReqs, r)
		text, _ := body["inputs"].(string)
		toks := []map[string]any{{"id": 101, "text": "[CLS]", "special": true, "start": nil, "stop": nil}}
		for i, tk := range spaceTokens(text) {
			toks = append(toks, map[string]any{"id": 1000 + i, "text": tk.text, "special": false, "start": tk.start, "stop": tk.end})
		}
		toks = append(toks, map[string]any{"id": 102, "text": "[SEP]", "special": true, "start": nil, "stop": nil})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([][]map[string]any{toks})
	})
	mux.HandleFunc("/embed_all", func(w http.ResponseWriter, r *http.Request) {
		body := f.record(&f.embedAllReqs, r)
		if f.embedAllStatus != 0 {
			w.WriteHeader(f.embedAllStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Input validation error: `inputs` must have less than 256 tokens", "error_type": "Validation"})
			return
		}
		text, _ := body["inputs"].(string)
		content := spaceTokens(text)
		// One position per token, specials included: [CLS] content... [SEP].
		positions := make([][]float64, 0, len(content)+2)
		positions = append(positions, make([]float64, f.dim))
		for i := range content {
			positions = append(positions, tokenVector(i, f.dim))
		}
		positions = append(positions, make([]float64, f.dim))
		if f.embedAllDropLast {
			positions = positions[:len(positions)-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([][][]float64{positions})
	})
	mux.HandleFunc("/embed", func(w http.ResponseWriter, r *http.Request) {
		f.embedCalls.Add(1)
		body := f.record(&f.embedReqs, r)
		if f.embedStatus != 0 {
			w.WriteHeader(f.embedStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Model is overloaded", "error_type": "Overloaded"})
			return
		}
		inputs, _ := body["inputs"].([]any)
		out := make([][]float64, len(inputs))
		for i := range inputs {
			v := make([]float64, f.dim)
			v[0] = 1 // unit norm
			out[i] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}

func newFake(t *testing.T, f *fakeTEI) (*fakeTEI, *tei.Client) {
	t.Helper()
	if f.pooling == "" {
		f.pooling = "mean"
	}
	if f.maxInputLength == 0 {
		f.maxInputLength = 256
	}
	if f.dim == 0 {
		f.dim = 4
	}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := tei.NewClient(srv.URL, "")
	c.InitialBackoff = time.Millisecond
	c.MaxBackoff = time.Millisecond
	return f, c
}

// TestEmbed_NativeContract pins the /embed request the client sends: the native
// {"inputs":[...],"normalize":true,"truncate":false} shape (never the OpenAI
// /v1/embeddings shape), one vector per input in input order, and no
// Authorization header for a credential-less endpoint.
func TestEmbed_NativeContract(t *testing.T) {
	f, c := newFake(t, &fakeTEI{})
	vecs, err := c.Embed(context.Background(), "ignored", model.EmbedDocument, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 || len(vecs[0]) != 4 {
		t.Fatalf("got %d vectors of dim %d, want 3 of dim 4", len(vecs), len(vecs[0]))
	}
	if len(f.embedReqs) != 1 {
		t.Fatalf("embed requests = %d, want 1 batch", len(f.embedReqs))
	}
	req := f.embedReqs[0]
	if req["normalize"] != true {
		t.Fatalf("normalize = %v, want true (query vectors must be unit-norm like the pooled document vectors)", req["normalize"])
	}
	if req["truncate"] != false {
		t.Fatalf("truncate = %v, want false (an over-long input must fail, not silently truncate)", req["truncate"])
	}
	if _, isOpenAI := req["input"]; isOpenAI || req["model"] != nil {
		t.Fatalf("request used the OpenAI shape, want the TEI native shape: %v", req)
	}
	if f.authHeaders[0] != "" {
		t.Fatalf("credential-less endpoint must get no Authorization header, got %q", f.authHeaders[0])
	}
}

// TestEmbed_BearerWhenConfigured pins that a configured api_key is sent as a
// Bearer token.
func TestEmbed_BearerWhenConfigured(t *testing.T) {
	f := &fakeTEI{pooling: "mean", maxInputLength: 256, dim: 4}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := tei.NewClient(srv.URL, "secret")
	if _, err := c.Embed(context.Background(), "", model.EmbedDocument, []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if f.authHeaders[0] != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", f.authHeaders[0])
	}
}

// TestEmbedDocumentTokens_RuneOffsetsAndSpecials pins the model.TokenEmbedder
// contract on the TEI facts: /tokenize gives BYTE offsets, the client returns
// RUNE offsets into the original document, and the [CLS]/[SEP] positions
// (which have no span) are dropped while every content token's vector is kept in
// reading order and aligned with /embed_all's positions.
func TestEmbedDocumentTokens_RuneOffsetsAndSpecials(t *testing.T) {
	f, c := newFake(t, &fakeTEI{})
	doc := "héllo wörld ünïcode" // multi-byte runes so byte and rune offsets differ
	out, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{doc})
	if err != nil {
		t.Fatalf("EmbedDocumentTokens: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d embeddings for 1 input", len(out))
	}
	te := out[0]
	if len(te.Vectors) != 3 || len(te.Offsets) != 3 || len(te.Ends) != 3 {
		t.Fatalf("want 3 content tokens (specials dropped), got vectors=%d offsets=%d ends=%d", len(te.Vectors), len(te.Offsets), len(te.Ends))
	}
	runes := []rune(doc)
	wantWords := []string{"héllo", "wörld", "ünïcode"}
	for i, w := range wantWords {
		got := string(runes[te.Offsets[i]:te.Ends[i]])
		if got != w {
			t.Fatalf("token %d rune span [%d,%d) = %q, want %q (byte offsets must be converted to rune offsets)", i, te.Offsets[i], te.Ends[i], got, w)
		}
		if te.Vectors[i][0] != float32(i+1) {
			t.Fatalf("token %d vector %v is not /embed_all position %d (alignment with /tokenize lost)", i, te.Vectors[i], i+1)
		}
	}
	if te.Ends[2] != utf8.RuneCountInString(doc) {
		t.Fatalf("last token end %d != rune length %d", te.Ends[2], utf8.RuneCountInString(doc))
	}
	// Request shapes.
	if len(f.tokenizeReqs) != 1 || f.tokenizeReqs[0]["add_special_tokens"] != true {
		t.Fatalf("tokenize must be called once with add_special_tokens=true so positions line up with /embed_all: %v", f.tokenizeReqs)
	}
	if len(f.embedAllReqs) != 1 || f.embedAllReqs[0]["truncate"] != false {
		t.Fatalf("embed_all must be called once with truncate=false (auto_truncate would misalign offsets): %v", f.embedAllReqs)
	}
	if f.embedCalls.Load() != 0 {
		t.Fatal("the token path must not call /embed")
	}
}

// TestEmbedDocumentTokens_RefusesNonMeanPooling pins SPEC 8.1.9: only a
// mean-pooling model puts the pooled query vector and the per-token vectors in
// one space, so a CLS-pooling server gets a NON-retryable error and no
// /embed_all call is made. The worker normally never reaches this: the probe
// (TokenEmbeddingsAvailable, tested below) refuses the server before any
// document is embedded and the corpus falls back as a whole; this per-call
// refusal is the defence in depth, and a worker that does hit it fails the
// representation's chunks rather than embedding them chunk-then-embed.
func TestEmbedDocumentTokens_RefusesNonMeanPooling(t *testing.T) {
	f, c := newFake(t, &fakeTEI{pooling: "cls"})
	_, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{"a b"})
	if err == nil {
		t.Fatal("expected an error for a cls-pooling model")
	}
	var pErr *model.ProviderError
	if !errors.As(err, &pErr) || pErr.Retryable {
		t.Fatalf("want a non-retryable ProviderError, got %v", err)
	}
	if !strings.Contains(err.Error(), "mean") || !strings.Contains(err.Error(), "cls") {
		t.Fatalf("error must name the required and the served pooling: %v", err)
	}
	if len(f.embedAllReqs) != 0 || len(f.tokenizeReqs) != 0 {
		t.Fatalf("no token calls may be made for an unsupported pooling: tokenize=%d embed_all=%d", len(f.tokenizeReqs), len(f.embedAllReqs))
	}
}

// TestEmbedDocumentTokens_WindowsLongDocument pins SPEC 8.1.9 "Long documents":
// a document over max_input_length is split into consecutive non-overlapping
// token windows (max - specials - slack tokens each), each window is tokenized
// and embedded on its own, and every returned offset is still relative to the
// WHOLE document, so the caller pools across windows without knowing they exist.
func TestEmbedDocumentTokens_WindowsLongDocument(t *testing.T) {
	// 16 - 2 specials - 8 slack = 6 content tokens per window; 15 words -> 6+6+3.
	f, c := newFake(t, &fakeTEI{maxInputLength: 16})
	words := []string{"w0", "w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8", "w9", "wA", "wB", "wC", "wD", "wE"}
	doc := strings.Join(words, " ")
	out, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{doc})
	if err != nil {
		t.Fatalf("EmbedDocumentTokens: %v", err)
	}
	te := out[0]
	if len(te.Vectors) != len(words) {
		t.Fatalf("got %d token vectors, want %d across all windows", len(te.Vectors), len(words))
	}
	if got := len(f.embedAllReqs); got != 3 {
		t.Fatalf("embed_all calls = %d, want 3 windows (6+6+3 tokens)", got)
	}
	// 1 whole-document tokenize to plan windows + 1 per window.
	if got := len(f.tokenizeReqs); got != 4 {
		t.Fatalf("tokenize calls = %d, want 4 (1 plan + 3 windows)", got)
	}
	runes := []rune(doc)
	for i, w := range words {
		if got := string(runes[te.Offsets[i]:te.Ends[i]]); got != w {
			t.Fatalf("token %d span [%d,%d) = %q, want %q (window offsets must be rebased to the document)", i, te.Offsets[i], te.Ends[i], got, w)
		}
	}
	// Within a window the fake numbers positions from 1, so a window boundary is
	// visible as the position counter restarting; the offsets above prove the
	// vectors still land on the right document tokens.
	if te.Vectors[6][0] != 1 || te.Vectors[12][0] != 1 {
		t.Fatalf("expected window restarts at tokens 6 and 12, got %v / %v", te.Vectors[6], te.Vectors[12])
	}
	// Windows must be consecutive and non-overlapping in the document.
	firstWindow, _ := f.embedAllReqs[0]["inputs"].(string)
	secondWindow, _ := f.embedAllReqs[1]["inputs"].(string)
	if firstWindow != strings.Join(words[:6], " ") || secondWindow != strings.Join(words[6:12], " ") {
		t.Fatalf("windows = %q / %q, want the first 6 and next 6 words", firstWindow, secondWindow)
	}
}

// TestEmbedDocumentTokens_AlignmentMismatchIsNonRetryable pins that a server
// whose /embed_all positions do not line up with its /tokenize tokens is
// rejected outright: a misaligned pool would silently attribute vectors to the
// wrong runes.
func TestEmbedDocumentTokens_AlignmentMismatchIsNonRetryable(t *testing.T) {
	_, c := newFake(t, &fakeTEI{embedAllDropLast: true})
	_, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{"a b c"})
	var pErr *model.ProviderError
	if err == nil || !errors.As(err, &pErr) || pErr.Retryable {
		t.Fatalf("want a non-retryable ProviderError for a token/vector count mismatch, got %v", err)
	}
}

// TestEmbedDocumentTokens_ValidationErrorIsNonRetryable pins TEI's 422
// Validation rejection (an input the server refuses) as non-retryable: the
// worker fails that representation's chunks with a reason (SPEC 8.1.9), never
// embeds them chunk-then-embed, and never retries them forever.
func TestEmbedDocumentTokens_ValidationErrorIsNonRetryable(t *testing.T) {
	f, c := newFake(t, &fakeTEI{embedAllStatus: http.StatusUnprocessableEntity})
	_, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{"a b"})
	var pErr *model.ProviderError
	if err == nil || !errors.As(err, &pErr) || pErr.Retryable || pErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want a non-retryable 422 ProviderError, got %v", err)
	}
	if !strings.Contains(err.Error(), "Validation") {
		t.Fatalf("error must carry TEI's error_type: %v", err)
	}
	if len(f.embedAllReqs) != 1 {
		t.Fatalf("a non-retryable status must not be retried: embed_all calls = %d", len(f.embedAllReqs))
	}
}

// TestEmbed_OverloadedIsRetryable pins TEI's 429 (Overloaded) as a retryable
// rate-limit error: the client retries with backoff and, once exhausted, hands
// the worker a Retryable ProviderError so the chunks stay pending (#932).
func TestEmbed_OverloadedIsRetryable(t *testing.T) {
	f, c := newFake(t, &fakeTEI{embedStatus: http.StatusTooManyRequests})
	c.MaxRetries = 2
	_, err := c.Embed(context.Background(), "", model.EmbedDocument, []string{"a"})
	var pErr *model.ProviderError
	if err == nil || !errors.As(err, &pErr) || !pErr.Retryable || pErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("want a retryable 429 ProviderError, got %v", err)
	}
	if pErr.Code != "TEI_RATE_LIMIT" {
		t.Fatalf("code = %q, want TEI_RATE_LIMIT", pErr.Code)
	}
	if got := f.embedCalls.Load(); got != 3 {
		t.Fatalf("embed attempts = %d, want 1 + 2 retries", got)
	}
}

// TestEmbed_AuthIsNonRetryable pins 401 as TEI_AUTH, non-retryable, one attempt.
func TestEmbed_AuthIsNonRetryable(t *testing.T) {
	f, c := newFake(t, &fakeTEI{embedStatus: http.StatusUnauthorized})
	_, err := c.Embed(context.Background(), "", model.EmbedDocument, []string{"a"})
	var pErr *model.ProviderError
	if err == nil || !errors.As(err, &pErr) || pErr.Retryable || pErr.Code != "TEI_AUTH" {
		t.Fatalf("want a non-retryable TEI_AUTH ProviderError, got %v", err)
	}
	if got := f.embedCalls.Load(); got != 1 {
		t.Fatalf("auth failure must not be retried: attempts = %d", got)
	}
}

// TestEmbed_RejectsZeroNormVector pins issue #703 at the TEI boundary: an
// all-zero "successful" vector is a malformed output, not a healthy embedding.
func TestEmbed_RejectsZeroNormVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([][]float64{{0, 0, 0, 0}})
	}))
	defer srv.Close()
	c := tei.NewClient(srv.URL, "")
	_, err := c.Embed(context.Background(), "", model.EmbedDocument, []string{"x"})
	var pErr *model.ProviderError
	if err == nil || !errors.As(err, &pErr) || pErr.Retryable {
		t.Fatalf("want a non-retryable malformed-output error, got %v", err)
	}
}

// TestEmbedDocumentTokens_EmptyDocumentMakesNoCalls pins that a whitespace-only
// document yields an empty token embedding (every chunk span then falls back)
// without a server round-trip that TEI would reject.
func TestEmbedDocumentTokens_EmptyDocumentMakesNoCalls(t *testing.T) {
	f, c := newFake(t, &fakeTEI{})
	out, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{"   \n  "})
	if err != nil {
		t.Fatalf("EmbedDocumentTokens: %v", err)
	}
	if len(out) != 1 || len(out[0].Vectors) != 0 {
		t.Fatalf("want one empty token embedding, got %+v", out)
	}
	if len(f.tokenizeReqs) != 0 || len(f.embedAllReqs) != 0 {
		t.Fatal("no request may be made for a whitespace-only document")
	}
}

// TestServerInfo_CachedOnce pins that /info is fetched once per client: a TEI
// server serves one model, so the answer cannot change under a running client.
func TestServerInfo_CachedOnce(t *testing.T) {
	var infoCalls atomic.Int32
	f := &fakeTEI{pooling: "mean", maxInputLength: 256, dim: 4}
	inner := f.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			infoCalls.Add(1)
		}
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := tei.NewClient(srv.URL, "")
	for i := 0; i < 3; i++ {
		if _, err := c.EmbedDocumentTokens(context.Background(), "", model.EmbedDocument, []string{"a b"}); err != nil {
			t.Fatalf("EmbedDocumentTokens #%d: %v", i, err)
		}
	}
	if got := infoCalls.Load(); got != 1 {
		t.Fatalf("/info fetched %d times, want 1", got)
	}
	info, err := c.ServerInfo(context.Background())
	if err != nil || info.Pooling != "mean" || info.MaxInputLength != 256 || info.ModelID != "fake/mini" {
		t.Fatalf("ServerInfo = %+v, %v", info, err)
	}
}

// TestTokenEmbeddingsAvailable_ProbesPoolingOnce pins the model.TokenEmbeddingProbe
// contract (SPEC 8.1.9): a mean-pooling server is available; a cls-pooling
// server is a DEFINITIVE refusal that names the served pooling, with err == nil,
// so the worker falls back corpus-wide before any document is embedded; neither
// answer makes a token call. The answer comes from the cached /info.
func TestTokenEmbeddingsAvailable_ProbesPoolingOnce(t *testing.T) {
	_, ok := any(&tei.Client{}).(model.TokenEmbeddingProbe)
	if !ok {
		t.Fatal("tei.Client must implement model.TokenEmbeddingProbe")
	}
	f, c := newFake(t, &fakeTEI{pooling: "mean"})
	avail, reason, err := c.TokenEmbeddingsAvailable(context.Background())
	if err != nil || !avail || reason != "" {
		t.Fatalf("mean pooling: avail=%v reason=%q err=%v, want true, \"\", nil", avail, reason, err)
	}
	if len(f.embedAllReqs) != 0 || len(f.tokenizeReqs) != 0 {
		t.Fatal("the probe must not make a token call")
	}

	f2, c2 := newFake(t, &fakeTEI{pooling: "cls"})
	avail, reason, err = c2.TokenEmbeddingsAvailable(context.Background())
	if err != nil || avail {
		t.Fatalf("cls pooling: avail=%v err=%v, want a definitive refusal (false, nil)", avail, err)
	}
	if !strings.Contains(reason, "mean") || !strings.Contains(reason, "cls") {
		t.Fatalf("the reason must name the required and the served pooling: %q", reason)
	}
	if len(f2.embedAllReqs) != 0 || len(f2.tokenizeReqs) != 0 {
		t.Fatal("a refused probe must not make a token call")
	}
}

// TestTokenEmbeddingsAvailable_UnreachableIsUnknown pins the other half: a
// server that cannot be reached is not a refusal. The probe returns err, the
// worker keeps the pooled path, and a transient outage never flips a pooled
// corpus to unpooled vectors.
func TestTokenEmbeddingsAvailable_UnreachableIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	c := tei.NewClient(srv.URL, "")
	c.InitialBackoff = time.Millisecond
	c.MaxBackoff = time.Millisecond
	avail, reason, err := c.TokenEmbeddingsAvailable(context.Background())
	if err == nil {
		t.Fatalf("an unreachable server must be an UNKNOWN answer (err), got avail=%v reason=%q", avail, reason)
	}
	if avail {
		t.Fatal("unknown must not report available")
	}
}
