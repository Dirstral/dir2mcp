// Package tei is a pure-Go client for a self-hosted Hugging Face Text
// Embeddings Inference (TEI) server addressed on its NATIVE surface (SPEC
// 8.1.1 `kind: tei`, dir2mcp#565). It is the first shipped embedder that
// implements model.TokenEmbedder, and therefore the first that can serve late
// chunking (SPEC 8.1.9): TEI's POST /embed_all returns one vector per token with
// no pooling, and POST /tokenize returns each token's offsets into the input, so
// the pooling step in internal/latechunk can select exactly the tokens of each
// chunk.
//
// Endpoints used (all relative to BaseURL, which is the server root, not /v1):
//
//   - GET  /info       served model id, its pooling and max_input_length.
//   - POST /embed      pooled, L2-normalized vectors (model.Embedder).
//   - POST /tokenize   token ids with BYTE offsets; special tokens carry null.
//   - POST /embed_all  [batch][sequence][hidden] raw token states, special
//     tokens included at their positions, no pooling, no normalization.
//
// Facts this client relies on, verified against the TEI source
// (core/src/tokenization.rs, router/src/http/server.rs) and a live 1.9.x server:
// /tokenize offsets are byte offsets from tokenizer.encode (the
// model.TokenEmbedding contract wants rune offsets, so they are converted here);
// /embed_all positions line up 1:1 with /tokenize's tokens for the same text with
// add_special_tokens=true; /embed's mean pooling averages every position,
// special tokens included, and normalizes.
//
// A TEI server also exposes an OpenAI-compatible /v1/embeddings; a `kind: openai`
// profile pointed at it keeps working but returns pooled vectors only, so it
// cannot serve late chunking. That is why this is a distinct kind.
//
// The endpoint may be credential-less (a box on a private network): a Bearer
// token is sent only when an api_key is configured. This client never logs
// document text, request bodies, or the API key.
package tei

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/providerhttp"
)

const (
	defaultRequestTimeout = 120 * time.Second
	defaultMaxRetries     = 3
	defaultInitialBackoff = 250 * time.Millisecond
	defaultMaxBackoff     = 2 * time.Second
	defaultBatchSize      = 32

	// DefaultModel is the identity label used when the profile names no model.
	// A TEI server serves exactly one model and its native requests carry no
	// model field, so the name is never sent on the wire; it exists so the embed
	// identity (SPEC 8.1.4) has a non-empty model component. Operators SHOULD
	// set embed_text_model to the served model id so the identity names it.
	DefaultModel = "tei"

	// PoolingMean is the /info pooling value late chunking requires (SPEC
	// 8.1.9): only then is the pooled query vector the normalized mean of the
	// same token states the document path pools, so both live in one space.
	PoolingMean = "mean"

	// maxEmbedAllResponseBytes caps an /embed_all body. One window of a
	// long-context model is up to ~8192 tokens x 1024 dims of ~11-byte JSON
	// floats (~90 MiB), so the shared 64 MiB JSON cap is too small here.
	maxEmbedAllResponseBytes int64 = 512 << 20

	// windowSlackTokens is subtracted from a window's token budget. A window's
	// text is re-tokenized by the server on its own, and a token cut at the
	// window edge can split differently in isolation than it did inside the
	// whole document, so a window filled to the exact limit could come back a
	// few tokens over and be rejected (422). The slack absorbs that.
	windowSlackTokens = 8

	codeFailed    = "TEI_FAILED"
	codeAuth      = "TEI_AUTH"
	codeRateLimit = "TEI_RATE_LIMIT"
)

// Client speaks the TEI native HTTP contract against a self-hosted base URL.
//
// Error codes:
//   - TEI_AUTH (non-retryable): upstream 401/403.
//   - TEI_RATE_LIMIT (retryable): upstream 429 (TEI's "overloaded").
//   - TEI_FAILED (retryable for network/5xx, non-retryable otherwise, including
//     TEI's 413/422 validation errors and 424 backend errors).
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client

	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// BatchSize bounds how many inputs one /embed request carries. Values <= 0
	// fall back to defaultBatchSize. TEI's own max_client_batch_size defaults to
	// 32, which is why the default here matches it.
	BatchSize int

	// DefaultEmbedModel is the identity label recorded when the profile names no
	// model (see DefaultModel). It is never sent on the wire.
	DefaultEmbedModel string

	infoMu sync.Mutex
	info   *Info
}

// compile-time assertions that *Client implements the model contracts.
var (
	_ model.Embedder      = (*Client)(nil)
	_ model.TokenEmbedder = (*Client)(nil)
)

// NewClient constructs a client with safe default retry/timeout settings.
// apiKey may be empty for a credential-less self-hosted endpoint.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:           strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:            strings.TrimSpace(apiKey),
		HTTPClient:        providerhttp.NewClient(defaultRequestTimeout),
		MaxRetries:        defaultMaxRetries,
		InitialBackoff:    defaultInitialBackoff,
		MaxBackoff:        defaultMaxBackoff,
		BatchSize:         defaultBatchSize,
		DefaultEmbedModel: DefaultModel,
	}
}

// Info is the subset of GET /info this client acts on.
type Info struct {
	// ModelID is the served model (e.g. "sentence-transformers/all-MiniLM-L6-v2").
	ModelID string
	// Pooling is the served model's pooling ("cls", "mean", "last_token", ...).
	Pooling string
	// MaxInputLength is the maximum number of tokens per request, special
	// tokens included; a longer input is rejected with 422 unless truncated.
	MaxInputLength int
}

type infoResponse struct {
	ModelID        string `json:"model_id"`
	MaxInputLength int    `json:"max_input_length"`
	ModelType      struct {
		Embedding *struct {
			Pooling string `json:"pooling"`
		} `json:"embedding"`
	} `json:"model_type"`
}

type embedRequest struct {
	Inputs    []string `json:"inputs"`
	Normalize bool     `json:"normalize"`
	Truncate  bool     `json:"truncate"`
}

type tokenizeRequest struct {
	Inputs           string `json:"inputs"`
	AddSpecialTokens bool   `json:"add_special_tokens"`
}

type embedAllRequest struct {
	Inputs   string `json:"inputs"`
	Truncate bool   `json:"truncate"`
}

// simpleToken is one /tokenize entry. Start/Stop are BYTE offsets into the
// request text and are null for special tokens.
type simpleToken struct {
	ID      int    `json:"id"`
	Text    string `json:"text"`
	Special bool   `json:"special"`
	Start   *int   `json:"start"`
	Stop    *int   `json:"stop"`
}

// Embed implements model.Embedder via POST /embed with normalize=true and
// truncate=false: a pooled, unit-norm vector per input, and an over-long input
// is an explicit error rather than a silently truncated vector. The role is
// accepted and ignored (a TEI model is symmetric, SPEC 8.1.5). Inputs are sent
// in BatchSize-sized batches, each retried with bounded exponential backoff.
func (c *Client) Embed(ctx context.Context, _ string, _ model.EmbedRole, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, &model.ProviderError{Code: codeFailed, Message: "missing tei base_url", Retryable: false}
	}
	batchSize := c.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	out := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		vectors, err := c.embedBatchWithRetry(ctx, inputs[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	// Reject empty / non-finite / zero-norm vectors at the provider boundary
	// (issue #703) so they never reach an index.
	if err := model.ValidateEmbedVectors(codeFailed, 0, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) embedBatchWithRetry(ctx context.Context, inputs []string) ([][]float32, error) {
	var vectors [][]float32
	err := c.withRetry(ctx, func() error {
		var err error
		vectors, err = c.embedBatch(ctx, inputs)
		return err
	})
	return vectors, err
}

func (c *Client) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Inputs: inputs, Normalize: true, Truncate: false})
	if err != nil {
		return nil, &model.ProviderError{Code: codeFailed, Message: "failed to marshal embed request", Retryable: false, Cause: err}
	}
	raw, status, err := c.postJSON(ctx, "/embed", body, providerhttp.MaxJSONResponseBytes)
	if err != nil {
		return nil, err
	}
	var parsed [][]float32
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &model.ProviderError{Code: codeFailed, Message: "failed to decode embed response", Retryable: false, StatusCode: status, Cause: err}
	}
	if len(parsed) != len(inputs) {
		return nil, &model.ProviderError{Code: codeFailed, Message: fmt.Sprintf("embed response size mismatch: got %d vectors for %d inputs", len(parsed), len(inputs)), Retryable: false, StatusCode: status}
	}
	return parsed, nil
}

// ServerInfo returns the served model's id, pooling and maximum input length
// (GET /info), fetched once and cached for the client's lifetime: a TEI server
// serves one model, so the answer cannot change under a running client.
func (c *Client) ServerInfo(ctx context.Context) (Info, error) {
	c.infoMu.Lock()
	defer c.infoMu.Unlock()
	if c.info != nil {
		return *c.info, nil
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return Info{}, &model.ProviderError{Code: codeFailed, Message: "missing tei base_url", Retryable: false}
	}
	var parsed infoResponse
	err := c.withRetry(ctx, func() error {
		raw, status, err := c.do(ctx, http.MethodGet, "/info", nil, providerhttp.MaxJSONResponseBytes)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return &model.ProviderError{Code: codeFailed, Message: "failed to decode /info response", Retryable: false, StatusCode: status, Cause: err}
		}
		return nil
	})
	if err != nil {
		return Info{}, err
	}
	info := Info{ModelID: parsed.ModelID, MaxInputLength: parsed.MaxInputLength}
	if parsed.ModelType.Embedding != nil {
		info.Pooling = strings.ToLower(strings.TrimSpace(parsed.ModelType.Embedding.Pooling))
	}
	if info.MaxInputLength <= 0 {
		return Info{}, &model.ProviderError{Code: codeFailed, Message: "/info reported no max_input_length", Retryable: false}
	}
	c.info = &info
	return info, nil
}

// TokenEmbeddingsAvailable implements model.TokenEmbeddingProbe (SPEC 8.1.9):
// it reads the served pooling from GET /info (cached for the client's lifetime)
// and answers whether this server can provide token embeddings that share a
// space with Embed's pooled vectors, which only a `mean`-pooling model does. A
// non-mean pooling, or an /info the client cannot accept (no max_input_length),
// is a definitive refusal with the reason; the worker then falls back
// corpus-wide, logged once, before any document is embedded. A retryable /info
// failure (the server is unreachable) is returned as err: the answer is unknown
// and the worker keeps the pooled path.
func (c *Client) TokenEmbeddingsAvailable(ctx context.Context) (bool, string, error) {
	info, err := c.ServerInfo(ctx)
	if err != nil {
		var pErr *model.ProviderError
		if errors.As(err, &pErr) && !pErr.Retryable {
			return false, err.Error(), nil
		}
		return false, "", err
	}
	if info.Pooling != PoolingMean {
		return false, fmt.Sprintf("late chunking requires a mean-pooling model; %q serves pooling %q", info.ModelID, info.Pooling), nil
	}
	return true, "", nil
}

// EmbedDocumentTokens implements model.TokenEmbedder (SPEC 8.1.9). For each
// input it returns one contextualized vector per token with the token's RUNE
// span in the input, special tokens excluded (they have no span). It refuses to
// serve a model whose pooling is not "mean", because only then do these vectors
// and Embed's pooled query vectors share one space. The worker normally learns
// that through TokenEmbeddingsAvailable before any document is embedded and
// falls back corpus-wide; this per-call check is the defence in depth for a
// server whose model changed under a running worker, and its non-retryable
// error then fails the affected representation's chunks with a reason.
//
// A document longer than the server's max_input_length is split into
// consecutive, non-overlapping token windows (SPEC 8.1.9 "Long documents"),
// each embedded on its own; every token still carries its offset in the WHOLE
// document, so the caller pools across windows transparently. The split is
// deterministic: it follows the server's own tokenization of the document.
func (c *Client) EmbedDocumentTokens(ctx context.Context, _ string, _ model.EmbedRole, inputs []string) ([]model.TokenEmbedding, error) {
	out := make([]model.TokenEmbedding, len(inputs))
	if len(inputs) == 0 {
		return out, nil
	}
	info, err := c.ServerInfo(ctx)
	if err != nil {
		return nil, err
	}
	if info.Pooling != PoolingMean {
		return nil, &model.ProviderError{
			Code:      codeFailed,
			Message:   fmt.Sprintf("late chunking requires a mean-pooling model; %q serves pooling %q", info.ModelID, info.Pooling),
			Retryable: false,
		}
	}
	for i, input := range inputs {
		te, err := c.embedDocumentTokens(ctx, info, input)
		if err != nil {
			return nil, err
		}
		out[i] = te
	}
	return out, nil
}

// embedDocumentTokens runs the token path for one document.
func (c *Client) embedDocumentTokens(ctx context.Context, info Info, doc string) (model.TokenEmbedding, error) {
	var te model.TokenEmbedding
	if strings.TrimSpace(doc) == "" {
		return te, nil // no tokens: every chunk span falls back (latechunk.ErrNoTokensInSpan)
	}
	docTokens, err := c.tokenize(ctx, doc)
	if err != nil {
		return te, err
	}
	windows, err := planWindows(doc, docTokens, info.MaxInputLength)
	if err != nil {
		return te, err
	}
	conv := newRuneOffsets(doc)
	for _, win := range windows {
		text := doc[win.byteStart:win.byteEnd]
		tokens := docTokens
		if len(windows) > 1 {
			// A window is re-tokenized on its own so its positions line up with
			// what /embed_all computes for exactly this text.
			if tokens, err = c.tokenize(ctx, text); err != nil {
				return te, err
			}
		}
		vectors, err := c.embedAll(ctx, text)
		if err != nil {
			return te, err
		}
		if len(vectors) != len(tokens) {
			return te, &model.ProviderError{
				Code:      codeFailed,
				Message:   fmt.Sprintf("/embed_all returned %d token vectors but /tokenize reported %d tokens for the same text", len(vectors), len(tokens)),
				Retryable: false,
			}
		}
		for k, tok := range tokens {
			if tok.Special || tok.Start == nil || tok.Stop == nil {
				continue
			}
			te.Vectors = append(te.Vectors, vectors[k])
			te.Offsets = append(te.Offsets, conv.runeAt(win.byteStart+*tok.Start, false))
			te.Ends = append(te.Ends, conv.runeAt(win.byteStart+*tok.Stop, true))
		}
	}
	return te, nil
}

// byteWindow is one embed window of a document, as a half-open byte range.
type byteWindow struct{ byteStart, byteEnd int }

// planWindows decides how a document is split for /embed_all given the server's
// tokenization of the whole document (special tokens included) and its
// max_input_length. A document that fits is one window covering all of it.
// Otherwise the non-special tokens are cut into consecutive groups of at most
// (max - specials - slack) tokens, and each window spans from its first token's
// byte start to its last token's byte stop; the whitespace between windows
// belongs to no token and is dropped, which changes no token vector.
func planWindows(doc string, docTokens []simpleToken, maxInputLength int) ([]byteWindow, error) {
	if len(docTokens) <= maxInputLength {
		return []byteWindow{{byteStart: 0, byteEnd: len(doc)}}, nil
	}
	specials := 0
	content := make([]simpleToken, 0, len(docTokens))
	for _, tok := range docTokens {
		if tok.Special || tok.Start == nil || tok.Stop == nil {
			specials++
			continue
		}
		content = append(content, tok)
	}
	budget := maxInputLength - specials - windowSlackTokens
	if budget <= 0 {
		return nil, &model.ProviderError{
			Code:      codeFailed,
			Message:   fmt.Sprintf("max_input_length %d leaves no room for a token window", maxInputLength),
			Retryable: false,
		}
	}
	var windows []byteWindow
	for i := 0; i < len(content); i += budget {
		j := i + budget
		if j > len(content) {
			j = len(content)
		}
		start, stop := *content[i].Start, *content[j-1].Stop
		if start < 0 || stop > len(doc) || stop <= start {
			return nil, &model.ProviderError{Code: codeFailed, Message: fmt.Sprintf("/tokenize reported an offset window [%d,%d) outside the %d-byte input", start, stop, len(doc)), Retryable: false}
		}
		windows = append(windows, byteWindow{byteStart: start, byteEnd: stop})
	}
	return windows, nil
}

// tokenize calls POST /tokenize with add_special_tokens=true for one text and
// returns its tokens in reading order with BYTE offsets.
func (c *Client) tokenize(ctx context.Context, text string) ([]simpleToken, error) {
	body, err := json.Marshal(tokenizeRequest{Inputs: text, AddSpecialTokens: true})
	if err != nil {
		return nil, &model.ProviderError{Code: codeFailed, Message: "failed to marshal tokenize request", Retryable: false, Cause: err}
	}
	var parsed [][]simpleToken
	err = c.withRetry(ctx, func() error {
		raw, status, err := c.postJSON(ctx, "/tokenize", body, providerhttp.MaxJSONResponseBytes)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return &model.ProviderError{Code: codeFailed, Message: "failed to decode tokenize response", Retryable: false, StatusCode: status, Cause: err}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(parsed) != 1 {
		return nil, &model.ProviderError{Code: codeFailed, Message: fmt.Sprintf("tokenize response has %d entries for 1 input", len(parsed)), Retryable: false}
	}
	return parsed[0], nil
}

// embedAll calls POST /embed_all with truncate=false for one text and returns
// one raw vector per token position, special tokens included. truncate is sent
// explicitly because a TEI server may run with auto_truncate=true, and a window
// silently cut short would misalign every following offset.
func (c *Client) embedAll(ctx context.Context, text string) ([][]float32, error) {
	body, err := json.Marshal(embedAllRequest{Inputs: text, Truncate: false})
	if err != nil {
		return nil, &model.ProviderError{Code: codeFailed, Message: "failed to marshal embed_all request", Retryable: false, Cause: err}
	}
	var parsed [][][]float32
	err = c.withRetry(ctx, func() error {
		raw, status, err := c.postJSON(ctx, "/embed_all", body, maxEmbedAllResponseBytes)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return &model.ProviderError{Code: codeFailed, Message: "failed to decode embed_all response", Retryable: false, StatusCode: status, Cause: err}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(parsed) != 1 {
		return nil, &model.ProviderError{Code: codeFailed, Message: fmt.Sprintf("embed_all response has %d entries for 1 input", len(parsed)), Retryable: false}
	}
	return parsed[0], nil
}

// runeOffsets converts BYTE offsets into a string to RUNE offsets in a single
// forward pass, which is all a reading-order token stream needs. A byte offset
// that falls inside a multi-byte rune (a byte-level tokenizer can split one)
// rounds down for a token start and up for a token end, so the token still
// covers the rune it touched and never becomes an empty or inverted span.
type runeOffsets struct {
	s       string
	bytePos int
	runePos int
}

func newRuneOffsets(s string) *runeOffsets { return &runeOffsets{s: s} }

func (r *runeOffsets) runeAt(b int, ceil bool) int {
	if b < 0 {
		return 0
	}
	if b > len(r.s) {
		b = len(r.s)
	}
	if b < r.bytePos {
		r.bytePos, r.runePos = 0, 0 // out-of-order token: rewind and rewalk
	}
	for r.bytePos < b {
		_, size := utf8.DecodeRuneInString(r.s[r.bytePos:])
		if r.bytePos+size > b {
			// b is inside this rune; do not consume it.
			if ceil {
				return r.runePos + 1
			}
			return r.runePos
		}
		r.bytePos += size
		r.runePos++
	}
	return r.runePos
}

// withRetry runs op, retrying a retryable ProviderError with bounded
// exponential backoff up to MaxRetries times. Any other error returns at once.
func (c *Client) withRetry(ctx context.Context, op func() error) error {
	maxRetries := c.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := c.wait(ctx, c.backoffForAttempt(attempt-1)); err != nil {
				return err
			}
		}
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		var pErr *model.ProviderError
		if errors.As(err, &pErr) && !pErr.Retryable {
			return err
		}
	}
	return lastErr
}

// postJSON POSTs body to path and returns the raw success body (bounded by
// limit) and the status code, or a classified ProviderError.
func (c *Client) postJSON(ctx context.Context, path string, body []byte, limit int64) ([]byte, int, error) {
	return c.do(ctx, http.MethodPost, path, body, limit)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, limit int64) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, 0, &model.ProviderError{Code: codeFailed, Message: "failed to build request", Retryable: false, Cause: err}
	}
	// Bearer auth is optional: only set it for credentialed endpoints.
	if key := strings.TrimSpace(c.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := providerhttp.ClientOrDefault(c.HTTPClient, defaultRequestTimeout).Do(req)
	if err != nil {
		return nil, 0, &model.ProviderError{Code: codeFailed, Message: "request failed", Retryable: true, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, httpError(resp)
	}
	raw, err := providerhttp.ReadLimitedBody(resp, limit, codeFailed)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// httpError classifies a non-200 TEI response. TEI's error body is
// {"error": "...", "error_type": "Validation|Overloaded|Backend|Tokenizer|..."};
// the message is passed through (it names the limit that was hit, never the
// input), the status decides retryability: 429 (Overloaded) and 5xx retry,
// 401/403 are auth, everything else (413/422 Validation, 424 Backend) is a
// deterministic rejection of this request.
func httpError(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	errMsg := strings.TrimSpace(string(bodyBytes))
	var parsed struct {
		Error     string `json:"error"`
		ErrorType string `json:"error_type"`
	}
	if json.Unmarshal(bodyBytes, &parsed) == nil && parsed.Error != "" {
		errMsg = parsed.Error
		if parsed.ErrorType != "" {
			errMsg = parsed.ErrorType + ": " + parsed.Error
		}
	}
	if errMsg == "" {
		errMsg = "upstream returned non-200 response"
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &model.ProviderError{Code: codeAuth, Message: errMsg, Retryable: false, StatusCode: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &model.ProviderError{Code: codeRateLimit, Message: errMsg, Retryable: true, StatusCode: resp.StatusCode}
	case resp.StatusCode >= http.StatusInternalServerError:
		return &model.ProviderError{Code: codeFailed, Message: errMsg, Retryable: true, StatusCode: resp.StatusCode}
	default:
		return &model.ProviderError{Code: codeFailed, Message: errMsg, Retryable: false, StatusCode: resp.StatusCode}
	}
}

func (c *Client) backoffForAttempt(attempt int) time.Duration {
	initial := c.InitialBackoff
	if initial <= 0 {
		initial = defaultInitialBackoff
	}
	maxBackoff := c.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultMaxBackoff
	}
	backoff := initial
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff >= maxBackoff {
			return maxBackoff
		}
	}
	return backoff
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
