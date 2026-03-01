package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"dir2mcp/internal/mistral"
)

func TestTranscribe_Integration_MistralAPI(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run integration tests")
	}

	apiKey := strings.TrimSpace(os.Getenv("MISTRAL_API_KEY"))
	if apiKey == "" {
		t.Skip("MISTRAL_API_KEY is not set")
	}
	samplePath := strings.TrimSpace(os.Getenv("MISTRAL_STT_SAMPLE"))
	if samplePath == "" {
		t.Skip("MISTRAL_STT_SAMPLE is not set (path to local audio sample)")
	}

	data, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read STT sample %s: %v", samplePath, err)
	}
	if len(data) == 0 {
		t.Fatalf("STT sample %s is empty", samplePath)
	}

	baseURL := strings.TrimSpace(os.Getenv("MISTRAL_BASE_URL"))
	client := mistral.NewClient(baseURL, apiKey)
	client.MaxRetries = 2

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout())
	defer cancel()

	out, err := client.Transcribe(ctx, samplePath, data)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("Transcribe returned empty output")
	}
}

func TestGenerate_Integration_MistralAPI(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run integration tests")
	}

	apiKey := strings.TrimSpace(os.Getenv("MISTRAL_API_KEY"))
	if apiKey == "" {
		t.Skip("MISTRAL_API_KEY is not set")
	}

	baseURL := strings.TrimSpace(os.Getenv("MISTRAL_BASE_URL"))
	client := mistral.NewClient(baseURL, apiKey)
	client.MaxRetries = 2

	ctx, cancel := context.WithTimeout(context.Background(), integrationTestTimeout())
	defer cancel()

	prompt := "Reply with exactly: dir2mcp-ok"
	out, err := client.Generate(ctx, prompt)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("Generate returned empty output")
	}
	if !strings.Contains(strings.ToLower(out), "dir2mcp-ok") {
		t.Fatalf("Generate output did not include expected token: %q", out)
	}
}
