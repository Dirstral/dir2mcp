package ingest

import (
	"strings"
	"testing"
)

func TestFlattenJSONForIndexing_MapAndArray(t *testing.T) {
	// service instance is only used for its logging helper; tests don't
	// actually inspect the logger output.
	s := &Service{}
	out := s.flattenJSONForIndexing(map[string]interface{}{
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
			t.Errorf("expected flattened output to contain %q, got %q", frag, out)
		}
	}
	if t.Failed() {
		// ensure the test exits non-successfully after reporting all missing fragments
		t.FailNow()
	}
}

func TestFlattenJSONForIndexing_MarshalErrorFallback(t *testing.T) {
	// Unsupported value (channel) cannot be JSON-marshaled.
	s := &Service{}
	out := s.flattenJSONForIndexing(make(chan int))
	if out != "" {
		t.Fatalf("expected empty output for marshal-error fallback, got %q", out)
	}
}
