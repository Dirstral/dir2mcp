package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dir2mcp/internal/model"
)

const (
	defaultBaseURL = "https://api.elevenlabs.io"
	defaultTimeout = 30 * time.Second
)

type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	VoiceID    string
}

type synthesizeRequest struct {
	Text string `json:"text"`
}

func NewClient(apiKey, voiceID string) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(defaultBaseURL), "/")
	return &Client{
		APIKey:     strings.TrimSpace(apiKey),
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
		VoiceID:    strings.TrimSpace(voiceID),
	}
}

func (c *Client) Synthesize(ctx context.Context, text string) ([]byte, error) {
	return c.SynthesizeWithVoice(ctx, text, c.VoiceID)
}

func (c *Client) SynthesizeWithVoice(ctx context.Context, text, voiceID string) ([]byte, error) {
	apiKey := strings.TrimSpace(c.APIKey)
	if apiKey == "" {
		return nil, &model.ProviderError{
			Code:      "ELEVENLABS_AUTH",
			Message:   "missing ElevenLabs API key",
			Retryable: false,
		}
	}

	voiceID = strings.TrimSpace(voiceID)
	if voiceID == "" {
		return nil, &model.ProviderError{
			Code:      "ELEVENLABS_FAILED",
			Message:   "voice_id is required",
			Retryable: false,
		}
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil, &model.ProviderError{
			Code:      "ELEVENLABS_FAILED",
			Message:   "text is required",
			Retryable: false,
		}
	}

	payload, err := json.Marshal(synthesizeRequest{Text: text})
	if err != nil {
		return nil, &model.ProviderError{
			Code:      "ELEVENLABS_FAILED",
			Message:   "failed to marshal TTS request",
			Retryable: false,
			Cause:     err,
		}
	}

	baseURL := strings.TrimSpace(c.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	reqURL := baseURL + "/v1/text-to-speech/" + url.PathEscape(voiceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, &model.ProviderError{
			Code:      "ELEVENLABS_FAILED",
			Message:   "failed to build TTS request",
			Retryable: false,
			Cause:     err,
		}
	}
	req.Header.Set("xi-api-key", apiKey)
	req.Header.Set("Accept", "audio/mpeg")
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &model.ProviderError{
			Code:      "ELEVENLABS_FAILED",
			Message:   "tts request failed",
			Retryable: true,
			Cause:     err,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &model.ProviderError{
			Code:       "ELEVENLABS_FAILED",
			Message:    "failed to read TTS response",
			Retryable:  true,
			StatusCode: resp.StatusCode,
			Cause:      err,
		}
	}

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return body, nil
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = fmt.Sprintf("elevenlabs tts returned status %d", resp.StatusCode)
	}
	return nil, mapProviderError(resp.StatusCode, message)
}

func mapProviderError(statusCode int, message string) error {
	pe := &model.ProviderError{
		Code:       "ELEVENLABS_FAILED",
		Message:    message,
		Retryable:  false,
		StatusCode: statusCode,
	}

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		pe.Code = "ELEVENLABS_AUTH"
		pe.Retryable = false
	case statusCode == http.StatusTooManyRequests:
		pe.Code = "ELEVENLABS_RATE_LIMIT"
		pe.Retryable = true
	case statusCode >= http.StatusInternalServerError:
		pe.Code = "ELEVENLABS_FAILED"
		pe.Retryable = true
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		pe.Code = "ELEVENLABS_FAILED"
		pe.Retryable = false
	default:
		pe.Code = "ELEVENLABS_FAILED"
		pe.Retryable = true
	}

	return pe
}
