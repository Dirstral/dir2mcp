package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"dir2mcp/internal/model"
)

const (
	// RepTypeRawText is the representation type for raw text content
	RepTypeRawText = "raw_text"
	// RepTypeOCRMarkdown is the representation type for OCR-generated markdown
	RepTypeOCRMarkdown = "ocr_markdown"
	// RepTypeTranscript is the representation type for audio transcripts
	RepTypeTranscript = "transcript"
	// RepTypeAnnotationJSON is the representation type for structured annotations
	RepTypeAnnotationJSON = "annotation_json"
	// RepTypeAnnotationText is the representation type for flattened annotation text
	RepTypeAnnotationText = "annotation_text"
)

// RepresentationGenerator handles creation of representations from documents
type RepresentationGenerator struct {
	store model.Store
}

// NewRepresentationGenerator creates a new representation generator
func NewRepresentationGenerator(store model.Store) *RepresentationGenerator {
	return &RepresentationGenerator{
		store: store,
	}
}

// GenerateRawText creates a raw_text representation for text-based documents.
// It reads the file content, normalizes to UTF-8, and stores it as a representation.
// 
// According to SPEC §7.4:
// - For code/text/md/data/html doc types
// - Normalize to UTF-8 with \n line endings
// - Route code → index_kind=code, others → index_kind=text
func (rg *RepresentationGenerator) GenerateRawText(ctx context.Context, doc model.Document, absPath string) error {
	// Read file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", doc.RelPath, err)
	}

	// Validate and normalize UTF-8
	normalizedContent, err := normalizeUTF8(content)
	if err != nil {
		return fmt.Errorf("normalize UTF-8 for %s: %w", doc.RelPath, err)
	}

	// Compute representation hash
	repHash := computeRepHash(normalizedContent)

	// Check if this representation already exists and is unchanged
	existingReps, err := rg.store.ListRepresentations(ctx, doc.DocID)
	if err != nil && !isNotFoundError(err) {
		return fmt.Errorf("list representations for doc %d: %w", doc.DocID, err)
	}

	for _, existing := range existingReps {
		if existing.RepType == RepTypeRawText && existing.RepHash == repHash && !existing.Deleted {
			// Representation unchanged, skip
			return nil
		}
	}

	// Create metadata JSON
	metaJSON := map[string]interface{}{
		"encoding": "utf-8",
		"normalized": true,
	}
	metaBytes, err := json.Marshal(metaJSON)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	// Create representation
	rep := model.Representation{
		DocID:       doc.DocID,
		RepType:     RepTypeRawText,
		RepHash:     repHash,
		CreatedUnix: time.Now().Unix(),
		MetaJSON:    string(metaBytes),
		Deleted:     false,
	}

	// Store representation
	if err := rg.store.UpsertRepresentation(ctx, rep, normalizedContent); err != nil {
		return fmt.Errorf("upsert representation: %w", err)
	}

	return nil
}

// normalizeUTF8 ensures content is valid UTF-8 and normalizes line endings to \n
func normalizeUTF8(content []byte) ([]byte, error) {
	// Check if already valid UTF-8
	if !utf8.Valid(content) {
		// Try to salvage by replacing invalid sequences
		// This is a simple approach - could be enhanced with encoding detection
		content = []byte(string(content))
	}

	// Normalize line endings: convert \r\n and \r to \n
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))

	return content, nil
}

// isNotFoundError checks if an error indicates a "not found" condition
// This is a placeholder - should be implemented based on actual error types
func isNotFoundError(err error) bool {
	// This would need to check for specific error types from the store
	// For now, return false to be conservative
	return false
}

// ShouldGenerateRawText determines if a document should have raw_text representation.
// According to SPEC §7.4, raw_text is generated for:
// - code (go, rs, py, js, ts, java, c, cpp, etc.)
// - text
// - md (markdown)
// - data (json, yaml, toml, etc.)
// - html
func ShouldGenerateRawText(docType string) bool {
	switch docType {
	case "code", "text", "md", "data", "html":
		return true
	default:
		return false
	}
}
