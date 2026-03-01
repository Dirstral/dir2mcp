package ingest

import (
	"strings"
	"testing"
)

func TestFlattenJSONForIndexing_MapAndArray(t *testing.T) {
	out := flattenJSONForIndexing(map[string]interface{}{
		"summary": "alpha",
		"topics":  []interface{}{"x", "y"},
		"meta": map[string]interface{}{
			"count": 2,
		},
	})

	wantContains := []string{
		"summary: alpha",
		"topics[0]: x",
		"topics[1]: y",
		"meta.count: 2",
	}
	for _, frag := range wantContains {
		if !strings.Contains(out, frag) {
			t.Fatalf("expected flattened output to contain %q, got %q", frag, out)
		}
	}
}

func TestFlattenJSONForIndexing_MarshalErrorFallback(t *testing.T) {
	// Unsupported value (channel) cannot be JSON-marshaled.
	out := flattenJSONForIndexing(make(chan int))
	if out != "" {
		t.Fatalf("expected empty output for marshal-error fallback, got %q", out)
	}
}
