package ingest

import (
	"path/filepath"
	"regexp"
	"strings"
)

// secretScanSampleBytes is the number of bytes to scan for secrets.
// This limits the search to the first N bytes to avoid scanning entire large files.
const secretScanSampleBytes int64 = 10 * 1024 // 10KB

// ClassifyDocType determines the document type based on file extension and path.
// Returns one of: code, text, md, pdf, image, audio, data, html, archive, binary_ignored
func ClassifyDocType(relPath string) string {
	ext := strings.ToLower(filepath.Ext(relPath))
	baseName := strings.ToLower(filepath.Base(relPath))

	// Code extensions
	codeExts := map[string]bool{
		".go":   true, ".rs": true, ".py": true, ".js": true, ".ts": true,
		".java": true, ".c": true, ".cpp": true, ".cc": true, ".cxx": true,
		".h": true, ".hpp": true, ".cs": true, ".rb": true, ".php": true,
		".swift": true, ".kt": true, ".scala": true, ".r": true,
		".sh": true, ".bash": true, ".zsh": true, ".fish": true,
		".pl": true, ".pm": true, ".lua": true, ".vim": true,
		".sql": true, ".graphql": true, ".proto": true,
	}
	if codeExts[ext] {
		return "code"
	}

	// Markdown
	if ext == ".md" || ext == ".markdown" {
		return "md"
	}

	// Text files
	textExts := map[string]bool{
		".txt": true, ".text": true, ".log": true,
	}
	if textExts[ext] || baseName == "readme" || baseName == "license" || baseName == "changelog" {
		return "text"
	}

	// Data/config files
	dataExts := map[string]bool{
		".json": true, ".yaml": true, ".yml": true, ".toml": true,
		".xml": true, ".csv": true, ".tsv": true, ".ini": true,
		".conf": true, ".config": true, ".properties": true,
		".env": true,
	}
	if dataExts[ext] {
		return "data"
	}

	// HTML
	if ext == ".html" || ext == ".htm" {
		return "html"
	}

	// PDF
	if ext == ".pdf" {
		return "pdf"
	}

	// Images
	imageExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".bmp": true, ".svg": true, ".webp": true, ".tiff": true,
		".ico": true,
	}
	if imageExts[ext] {
		return "image"
	}

	// Audio
	audioExts := map[string]bool{
		".mp3": true, ".wav": true, ".m4a": true, ".aac": true,
		".flac": true, ".ogg": true, ".wma": true,
	}
	if audioExts[ext] {
		return "audio"
	}

	// Archives
	archiveExts := map[string]bool{
		".zip": true, ".tar": true, ".gz": true, ".tgz": true,
		".bz2": true, ".xz": true, ".7z": true, ".rar": true,
	}
	if archiveExts[ext] {
		return "archive"
	}

	// Everything else is treated as binary_ignored
	return "binary_ignored"
}

// compileSecretPatterns compiles a list of regex patterns for secret detection.
// Returns compiled patterns or an error if any pattern is invalid.
func compileSecretPatterns(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for i, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, &InvalidPatternError{
				Pattern: pattern,
				Index:   i,
				Err:     err,
			}
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// InvalidPatternError is returned when a secret pattern cannot be compiled
type InvalidPatternError struct {
	Pattern string
	Index   int
	Err     error
}

func (e *InvalidPatternError) Error() string {
	return "invalid secret pattern at index " + string(rune(e.Index)) + ": " + e.Pattern + ": " + e.Err.Error()
}

func (e *InvalidPatternError) Unwrap() error {
	return e.Err
}

// hasSecretMatch checks if content matches any of the provided secret patterns.
// Returns true if a match is found, false otherwise.
func hasSecretMatch(content []byte, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 {
		return false
	}

	for _, pattern := range patterns {
		if pattern.Match(content) {
			return true
		}
	}
	return false
}

// matchesAnyPathExclude checks if a file path matches any of the exclusion patterns.
// Patterns can be simple glob patterns using * and **.
func matchesAnyPathExclude(relPath string, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}

	// Normalize path to use forward slashes
	normalizedPath := filepath.ToSlash(relPath)

	for _, pattern := range excludes {
		if matchesGlobPattern(normalizedPath, pattern) {
			return true
		}
	}
	return false
}

// matchesGlobPattern checks if a path matches a glob pattern.
// Supports:
// - * for matching any characters except /
// - ** for matching any characters including /
// - exact string matches
func matchesGlobPattern(path, pattern string) bool {
	// Normalize pattern to use forward slashes
	pattern = filepath.ToSlash(pattern)

	// Simple exact match
	if pattern == path {
		return true
	}

	// Check if pattern contains wildcards
	if !strings.Contains(pattern, "*") {
		// No wildcards, check for prefix/suffix/contains matches
		if strings.HasPrefix(pattern, "**") {
			suffix := strings.TrimPrefix(pattern, "**")
			return strings.HasSuffix(path, suffix)
		}
		if strings.HasSuffix(pattern, "**") {
			prefix := strings.TrimSuffix(pattern, "**")
			return strings.HasPrefix(path, prefix)
		}
		return false
	}

	// Convert glob pattern to regex
	// ** matches any characters including /
	// * matches any characters except /
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, `\*\*`, ".*")
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, "[^/]*")
	regexPattern = "^" + regexPattern + "$"

	matched, err := regexp.MatchString(regexPattern, path)
	if err != nil {
		// If regex fails, fall back to simple contains check
		return strings.Contains(path, strings.ReplaceAll(pattern, "*", ""))
	}
	return matched
}
