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
}

func TestFlattenJSONForIndexing_MarshalErrorFallback(t *testing.T) {
	// Unsupported value (channel) cannot be JSON-marshaled.
	s := &Service{}
	out := s.flattenJSONForIndexing(make(chan int))
	if out != "" {
		t.Fatalf("expected empty output for marshal-error fallback, got %q", out)
	}
}

func TestFlattenJSONForIndexing_TopLevelScalar(t *testing.T) {
	// Use a table-driven approach so it's easy to add new scalar inputs.
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{name: "string", input: "hello", want: "hello"},
		{name: "number", input: 42, want: "42"},
		{name: "float", input: 3.14, want: "3.14"},
		{name: "bool-true", input: true, want: "true"},
		{name: "bool-false", input: false, want: "false"},
		{name: "nil", input: nil, want: "null"},
	}

	s := &Service{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if out := s.flattenJSONForIndexing(tc.input); out != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, out)
			}
		})
	}
}
