package mistral

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dirstral/dir2mcp/internal/model"
)

const (
	defaultBaseURL        = "https://api.mistral.ai"
	defaultBatchSize      = 32
	defaultRequestTimeout = 30 * time.Second
	defaultMaxRetries     = 3
	defaultInitialBackoff = 250 * time.Millisecond
	defaultMaxBackoff     = 2 * time.Second
)

// Client provides Mistral API integrations.
//
// Embedding defaults:
// - base URL: https://api.mistral.ai
// - batch size: 32
// - request timeout: 30s
// - max retries: 3
// - backoff: 250ms exponential up to 2s
//
// Embed error codes:
// - MISTRAL_AUTH (non-retryable): missing/invalid credentials (401/403)
// - MISTRAL_RATE_LIMIT (retryable): upstream 429 responses
// - MISTRAL_FAILED (retryable for network/5xx, non-retryable for other 4xx/validation)
type Client struct {
	BaseURL        string
	APIKey         string
	HTTPClient     *http.Client
	BatchSize      int
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// NewClient constructs a client with safe default retry/timeout settings.
func NewClient(baseURL, apiKey string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		APIKey:         apiKey,
		HTTPClient:     &http.Client{Timeout: defaultRequestTimeout},
		BatchSize:      defaultBatchSize,
		MaxRetries:     defaultMaxRetries,
		InitialBackoff: defaultInitialBackoff,
		MaxBackoff:     defaultMaxBackoff,
	}
}

func (c *Client) Embed(ctx context.Context, modelName string, inputs []string) ([][]float32, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, &model.ProviderError{
			Code:      "MISTRAL_AUTH",
			Message:   "missing Mistral API key",
			Retryable: false,
		}
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, &model.ProviderError{
			Code:      "MISTRAL_FAILED",
			Message:   "model name is required",
			Retryable: false,
		}
	}
	if len(inputs) == 0 {
		return [][]float32{}, nil
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

		vectors, err := c.embedBatchWithRetry(ctx, modelName, inputs[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}

	return out, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedDataItem struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type embedResponse struct {
	Data []embedDataItem `json:"data"`
}

func (c *Client) embedBatchWithRetry(ctx context.Context, modelName string, inputs []string) ([][]float32, error) {
	maxRetries := c.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		vectors, err := c.embedBatch(ctx, modelName, inputs)
		if err == nil {
			return vectors, nil
		}
		lastErr = err

		var providerErr *model.ProviderError
		if !errors.As(err, &providerErr) || !providerErr.Retryable || attempt == maxRetries {
			return nil, err
		}

		backoff := c.backoffForAttempt(attempt)
		if waitErr := c.wait(ctx, backoff); waitErr != nil {
			return nil, waitErr
		}
	}

	return nil, lastErr
}

func (c *Client) embedBatch(ctx context.Context, modelName string, inputs []string) ([][]float32, error) {
	reqPayload := embedRequest{
		Model: modelName,
		Input: inputs,
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, &model.ProviderError{
			Code:      "MISTRAL_FAILED",
			Message:   "failed to marshal embedding request",
			Retryable: false,
			Cause:     err,
		}
	}

	url := c.BaseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &model.ProviderError{
			Code:      "MISTRAL_FAILED",
			Message:   "failed to build embedding request",
			Retryable: false,
			Cause:     err,
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &model.ProviderError{
			Code:      "MISTRAL_FAILED",
			Message:   "embedding request failed",
			Retryable: true,
			Cause:     err,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		errMsg := strings.TrimSpace(string(bodyBytes))
		if errMsg == "" {
			errMsg = "upstream returned non-200 response"
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, &model.ProviderError{
				Code:       "MISTRAL_AUTH",
				Message:    errMsg,
				Retryable:  false,
				StatusCode: resp.StatusCode,
			}
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, &model.ProviderError{
				Code:       "MISTRAL_RATE_LIMIT",
				Message:    errMsg,
				Retryable:  true,
				StatusCode: resp.StatusCode,
			}
		case resp.StatusCode >= http.StatusInternalServerError:
			return nil, &model.ProviderError{
				Code:       "MISTRAL_FAILED",
				Message:    errMsg,
				Retryable:  true,
				StatusCode: resp.StatusCode,
			}
		default:
			return nil, &model.ProviderError{
				Code:       "MISTRAL_FAILED",
				Message:    errMsg,
				Retryable:  false,
				StatusCode: resp.StatusCode,
			}
		}
	}

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, &model.ProviderError{
			Code:       "MISTRAL_FAILED",
			Message:    "failed to decode embedding response",
			Retryable:  false,
			StatusCode: resp.StatusCode,
			Cause:      err,
		}
	}

	if len(parsed.Data) != len(inputs) {
		return nil, &model.ProviderError{
			Code:       "MISTRAL_FAILED",
			Message:    fmt.Sprintf("embedding response size mismatch: got %d vectors for %d inputs", len(parsed.Data), len(inputs)),
			Retryable:  false,
			StatusCode: resp.StatusCode,
		}
	}

	vectors := make([][]float32, len(inputs))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(inputs) {
			return nil, &model.ProviderError{
				Code:       "MISTRAL_FAILED",
				Message:    "embedding response contains invalid index",
				Retryable:  false,
				StatusCode: resp.StatusCode,
			}
		}
		vector := make([]float32, len(item.Embedding))
		for i, val := range item.Embedding {
			vector[i] = float32(val)
		}
		vectors[item.Index] = vector
	}

	return vectors, nil
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
	if backoff > maxBackoff {
		return maxBackoff
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

func (c *Client) Extract(ctx context.Context, relPath string, data []byte) (string, error) {
	_ = ctx
	_ = relPath
	_ = data
	return "", model.ErrNotImplemented
}

func (c *Client) Transcribe(ctx context.Context, relPath string, data []byte) (string, error) {
	_ = ctx
	_ = relPath
	_ = data
	return "", model.ErrNotImplemented
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "", model.ErrNotImplemented
}
