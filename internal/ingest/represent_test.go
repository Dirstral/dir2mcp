package ingest

import (
	"bytes"
	"testing"
)

func TestNormalizeUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
		wantErr  bool
	}{
		{
			name:     "already valid UTF-8 with LF",
			input:    []byte("hello\nworld"),
			expected: []byte("hello\nworld"),
			wantErr:  false,
		},
		{
			name:     "CRLF to LF",
			input:    []byte("hello\r\nworld"),
			expected: []byte("hello\nworld"),
			wantErr:  false,
		},
		{
			name:     "CR to LF",
			input:    []byte("hello\rworld"),
			expected: []byte("hello\nworld"),
			wantErr:  false,
		},
		{
			name:     "mixed line endings",
			input:    []byte("line1\r\nline2\rline3\nline4"),
			expected: []byte("line1\nline2\nline3\nline4"),
			wantErr:  false,
		},
		{
			name:     "empty content",
			input:    []byte{},
			expected: []byte{},
			wantErr:  false,
		},
		{
			name:     "valid UTF-8 with special chars",
			input:    []byte("Hello 世界 🌍"),
			expected: []byte("Hello 世界 🌍"),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizeUTF8(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("normalizeUTF8() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !bytes.Equal(result, tt.expected) {
				t.Errorf("normalizeUTF8() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestShouldGenerateRawText(t *testing.T) {
	tests := []struct {
		name     string
		docType  string
		expected bool
	}{
		// Should generate raw_text
		{"code", "code", true},
		{"text", "text", true},
		{"markdown", "md", true},
		{"data", "data", true},
		{"html", "html", true},
		
		// Should NOT generate raw_text
		{"pdf", "pdf", false},
		{"image", "image", false},
		{"audio", "audio", false},
		{"archive", "archive", false},
		{"binary_ignored", "binary_ignored", false},
		{"unknown", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldGenerateRawText(tt.docType)
			if result != tt.expected {
				t.Errorf("ShouldGenerateRawText(%q) = %v, want %v", tt.docType, result, tt.expected)
			}
		})
	}
}

func TestRepTypeConstants(t *testing.T) {
	// Verify constants are defined with expected values per SPEC
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"raw_text", RepTypeRawText, "raw_text"},
		{"ocr_markdown", RepTypeOCRMarkdown, "ocr_markdown"},
		{"transcript", RepTypeTranscript, "transcript"},
		{"annotation_json", RepTypeAnnotationJSON, "annotation_json"},
		{"annotation_text", RepTypeAnnotationText, "annotation_text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("Constant %s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// Mock implementation for integration testing
// (This would be in a separate test file in practice)
type mockStore struct {
	documents       map[string]mockDocument
	representations map[int64][]mockRepresentation
}

type mockDocument struct {
	DocID       int64
	RelPath     string
	ContentHash string
	Status      string
}

type mockRepresentation struct {
	RepID   int64
	DocID   int64
	RepType string
	RepHash string
}

// Example integration test structure (implementation would be in a separate file)
func TestRepresentationGeneratorIntegration(t *testing.T) {
	// This would test the full flow with a mock store
	t.Skip("Integration test - implement with actual mock store")
	
	// Example test flow:
	// 1. Create mock store
	// 2. Create document
	// 3. Generate raw_text representation
	// 4. Verify representation was stored correctly
	// 5. Generate again (should be skipped due to unchanged hash)
	// 6. Verify no duplicate representation
}
