package tests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"dir2mcp/internal/elevenlabs"
	"dir2mcp/internal/model"
)

type elevenLabsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f elevenLabsRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestElevenLabsSynthesize_ReturnsAudioBytes(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotAuth   string
		gotBody   string
	)

	rt := elevenLabsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("xi-api-key")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("fake-mp3")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := elevenlabs.NewClient("test-api-key", "voice-default")
	client.HTTPClient = &http.Client{Transport: rt}

	audio, err := client.Synthesize(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}
	if string(audio) != "fake-mp3" {
		t.Fatalf("unexpected synthesized bytes: %q", string(audio))
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("unexpected method: %q", gotMethod)
	}
	if gotPath != "/v1/text-to-speech/voice-default" {
		t.Fatalf("unexpected request path: %q", gotPath)
	}
	if gotAuth != "test-api-key" {
		t.Fatalf("unexpected xi-api-key header: %q", gotAuth)
	}
	if !strings.Contains(gotBody, "hello world") {
		t.Fatalf("request body missing input text: %q", gotBody)
	}
}

func TestElevenLabsSynthesize_Maps401ToAuthError(t *testing.T) {
	rt := elevenLabsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(bytes.NewBufferString("unauthorized")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := elevenlabs.NewClient("test-api-key", "voice-default")
	client.HTTPClient = &http.Client{Transport: rt}

	_, err := client.Synthesize(context.Background(), "hello world")
	if err == nil {
		t.Fatal("expected provider error")
	}
	var providerErr *model.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "ELEVENLABS_AUTH" {
		t.Fatalf("unexpected code: %s", providerErr.Code)
	}
	if providerErr.Retryable {
		t.Fatal("expected non-retryable auth error")
	}
}

func TestElevenLabsSynthesize_Maps429ToRateLimitError(t *testing.T) {
	rt := elevenLabsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(bytes.NewBufferString("rate limited")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := elevenlabs.NewClient("test-api-key", "voice-default")
	client.HTTPClient = &http.Client{Transport: rt}

	_, err := client.Synthesize(context.Background(), "hello world")
	if err == nil {
		t.Fatal("expected provider error")
	}
	var providerErr *model.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "ELEVENLABS_RATE_LIMIT" {
		t.Fatalf("unexpected code: %s", providerErr.Code)
	}
	if !providerErr.Retryable {
		t.Fatal("expected retryable rate-limit error")
	}
}

func TestElevenLabsSynthesize_Maps5xxToRetryableFailure(t *testing.T) {
	rt := elevenLabsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(bytes.NewBufferString("upstream unavailable")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := elevenlabs.NewClient("test-api-key", "voice-default")
	client.HTTPClient = &http.Client{Transport: rt}

	_, err := client.Synthesize(context.Background(), "hello world")
	if err == nil {
		t.Fatal("expected provider error")
	}
	var providerErr *model.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "ELEVENLABS_FAILED" {
		t.Fatalf("unexpected code: %s", providerErr.Code)
	}
	if !providerErr.Retryable {
		t.Fatal("expected retryable failure for 5xx")
	}
}
