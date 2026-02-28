package ingest

import (
	"testing"
)

// Test hash computation functions
func TestComputeContentHash(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		expected string
	}{
		{
			name:     "empty content",
			content:  []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // SHA256 of empty string
		},
		{
			name:     "simple text",
			content:  []byte("hello world"),
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeContentHash(tt.content)
			if result != tt.expected {
				t.Errorf("computeContentHash() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestComputeRepHash(t *testing.T) {
	// Should produce same hash as computeContentHash since they use same algorithm
	content := []byte("test representation")
	contentHash := computeContentHash(content)
	repHash := computeRepHash(content)

	if contentHash != repHash {
		t.Errorf("computeRepHash() = %v, want %v", repHash, contentHash)
	}
}

func TestNeedsReprocessing(t *testing.T) {
	tests := []struct {
		name         string
		oldHash      string
		newHash      string
		forceReindex bool
		expected     bool
	}{
		{
			name:         "new document (no old hash)",
			oldHash:      "",
			newHash:      "abc123",
			forceReindex: false,
			expected:     true,
		},
		{
			name:         "unchanged document",
			oldHash:      "abc123",
			newHash:      "abc123",
			forceReindex: false,
			expected:     false,
		},
		{
			name:         "changed document",
			oldHash:      "abc123",
			newHash:      "def456",
			forceReindex: false,
			expected:     true,
		},
		{
			name:         "force reindex unchanged",
			oldHash:      "abc123",
			newHash:      "abc123",
			forceReindex: true,
			expected:     true,
		},
		{
			name:         "force reindex changed",
			oldHash:      "abc123",
			newHash:      "def456",
			forceReindex: true,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsReprocessing(tt.oldHash, tt.newHash, tt.forceReindex)
			if result != tt.expected {
				t.Errorf("needsReprocessing() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Test document type classification
func TestClassifyDocType(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		expected string
	}{
		// Code files
		{"go file", "main.go", "code"},
		{"python file", "script.py", "code"},
		{"javascript file", "app.js", "code"},
		{"typescript file", "component.ts", "code"},
		{"rust file", "lib.rs", "code"},
		// uppercase extensions and nested paths should still detect as code
		{"upper-case go", "MAIN.GO", "code"},
		{"upper-case python", "Script.PY", "code"},
		{"nested go", "src/lib/utils.go", "code"},

		// Markdown
		{"markdown", "README.md", "md"},
		{"markdown alt", "docs.markdown", "md"},
		{"nested markdown", "docs/README.md", "md"},

		// Text
		{"text file", "notes.txt", "text"},
		{"readme no ext", "README", "text"},
		{"license no ext", "LICENSE", "text"},

		// Data/config
		{"json", "package.json", "data"},
		{"yaml", "config.yaml", "data"},
		{"toml", "Cargo.toml", "data"},
		{"env", ".env", "data"},
		{"env local", ".env.local", "data"},
		{"env prod", ".env.production", "data"},

		// HTML
		{"html", "index.html", "html"},

		// PDF
		{"pdf", "document.pdf", "pdf"},

		// Images
		{"png", "image.png", "image"},
		{"jpg", "photo.jpg", "image"},
		{"jpeg", "photo.jpeg", "image"},

		// Audio
		{"mp3", "song.mp3", "audio"},
		{"wav", "recording.wav", "audio"},

		// Archives
		{"zip", "archive.zip", "archive"},
		{"tar", "backup.tar", "archive"},

		// Binary
		{"binary", "app.exe", "binary_ignored"},
		{"dll", "library.dll", "binary_ignored"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyDocType(tt.relPath)
			if result != tt.expected {
				t.Errorf("ClassifyDocType(%q) = %v, want %v", tt.relPath, result, tt.expected)
			}
		})
	}
}

// Test secret pattern compilation
func TestCompileSecretPatterns(t *testing.T) {
	tests := []struct {
		name      string
		patterns  []string
		wantError bool
	}{
		{
			name:      "empty patterns",
			patterns:  []string{},
			wantError: false,
		},
		{
			name:      "valid patterns",
			patterns:  []string{`AKIA[0-9A-Z]{16}`, `sk_[a-z0-9]{32}`},
			wantError: false,
		},
		{
			name:      "invalid regex",
			patterns:  []string{`[invalid`},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compileSecretPatterns(tt.patterns)
			if tt.wantError {
				if err == nil {
					t.Errorf("compileSecretPatterns() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("compileSecretPatterns() unexpected error: %v", err)
				}
				if len(result) != len(tt.patterns) {
					t.Errorf("compileSecretPatterns() returned %d patterns, want %d", len(result), len(tt.patterns))
				}
			}
		})
	}
}

// Test secret matching
func TestHasSecretMatch(t *testing.T) {
	patterns, err := compileSecretPatterns([]string{
		`AKIA[0-9A-Z]{16}`, // AWS Access Key
		`sk_[a-z0-9]{32}`,  // API key format
	})
	if err != nil {
		t.Fatalf("Failed to compile patterns: %v", err)
	}

	tests := []struct {
		name     string
		content  []byte
		expected bool
	}{
		{
			name:     "no secrets",
			content:  []byte("This is normal text"),
			expected: false,
		},
		{
			name:     "AWS access key",
			content:  []byte("export AWS_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE"),
			expected: true,
		},
		{
			name:     "API key",
			content:  []byte("api_key = sk_12345678901234567890123456789012"),
			expected: true,
		},
		{
			name:     "partial match not enough",
			content:  []byte("AKIA123"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasSecretMatch(tt.content, patterns)
			if result != tt.expected {
				t.Errorf("hasSecretMatch() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Test path exclusion matching
func TestMatchesAnyPathExclude(t *testing.T) {
	excludes := []string{
		"**/.git/**",
		"**/node_modules/**",
		"**/*.pem",
		"**/.env",
	}

	tests := []struct {
		name     string
		relPath  string
		expected bool
	}{
		{
			name:     "git directory",
			relPath:  ".git/config",
			expected: true,
		},
		{
			name:     "nested git",
			relPath:  "project/.git/HEAD",
			expected: true,
		},
		{
			name:     "node_modules",
			relPath:  "node_modules/package/index.js",
			expected: true,
		},
		{
			name:     "pem file",
			relPath:  "certs/private.pem",
			expected: true,
		},
		{
			name:     "env file",
			relPath:  ".env",
			expected: true,
		},
		{
			name:     "normal file",
			relPath:  "src/main.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesAnyPathExclude(tt.relPath, excludes)
			if result != tt.expected {
				t.Errorf("matchesAnyPathExclude(%q) = %v, want %v", tt.relPath, result, tt.expected)
			}
		})
	}
}

// Test glob pattern matching
func TestMatchesGlobPattern(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		pattern  string
		expected bool
	}{
		{
			name:     "exact match",
			filePath: ".env",
			pattern:  ".env",
			expected: true,
		},
		{
			name:     "star single segment",
			filePath: "test.txt",
			pattern:  "*.txt",
			expected: true,
		},
		{
			name:     "star no match",
			filePath: "dir/test.txt",
			pattern:  "*.txt",
			expected: false,
		},
		{
			name:     "double star prefix",
			filePath: "a/b/c/file.js",
			pattern:  "**/file.js",
			expected: true,
		},
		{
			name:     "double star suffix",
			filePath: ".git/config",
			pattern:  ".git/**",
			expected: true,
		},
		{
			name:     "double star middle",
			filePath: "src/components/Button.jsx",
			pattern:  "**/*.jsx",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesGlobPattern(tt.filePath, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesGlobPattern(%q, %q) = %v, want %v", tt.filePath, tt.pattern, result, tt.expected)
			}
		})
	}
}
