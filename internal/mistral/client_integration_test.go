package mistral

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEmbed_Integration_MistralAPI(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run integration tests")
	}

	apiKey := strings.TrimSpace(os.Getenv("MISTRAL_API_KEY"))
	if apiKey == "" {
		t.Skip("MISTRAL_API_KEY is not set")
	}

	baseURL := strings.TrimSpace(os.Getenv("MISTRAL_BASE_URL"))
	client := NewClient(baseURL, apiKey)
	client.BatchSize = 2
	client.MaxRetries = 2

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inputs := []string{
		"dir2mcp integration test sentence one",
		"dir2mcp integration test sentence two",
	}

	vectors, err := client.Embed(ctx, "mistral-embed", inputs)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("unexpected vector count: got %d want %d", len(vectors), len(inputs))
	}

	for i, vec := range vectors {
		if len(vec) == 0 {
			t.Fatalf("vector %d is empty", i)
		}
	}
}
