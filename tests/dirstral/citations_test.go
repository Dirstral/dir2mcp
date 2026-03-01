package test

import (
	"testing"

	"dir2mcp/internal/dirstral/mcp"
)

func TestCitationLines(t *testing.T) {
	got := mcp.CitationForSpan("src/main.go", map[string]any{"kind": "lines", "start_line": 10, "end_line": 20})
	if got != "[src/main.go:L10-L20]" {
		t.Fatalf("unexpected citation: %s", got)
	}
}

func TestCitationPage(t *testing.T) {
	got := mcp.CitationForSpan("notes.pdf", map[string]any{"kind": "page", "page": 4})
	if got != "[notes.pdf#p=4]" {
		t.Fatalf("unexpected citation: %s", got)
	}
}

func TestCitationTime(t *testing.T) {
	got := mcp.CitationForSpan("call.wav", map[string]any{"kind": "time", "start_ms": 15000, "end_ms": 32500})
	if got != "[call.wav@t=00:15-00:32]" {
		t.Fatalf("unexpected citation: %s", got)
	}
}
