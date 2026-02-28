package ingest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
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
	store model.RepresentationStore
}

// RepresentationGenerator handles creation of representations from documents.
// The backing store must satisfy model.RepresentationStore which is defined
// in the model package so that both ingest and store can depend on the same
// interface without forming a cyclic dependency.

// (no local interface required – model.RepresentationStore already declares
// UpsertRepresentation, InsertChunkWithSpans, SoftDeleteChunksFromOrdinal and
// WithTx.)

// NewRepresentationGenerator creates a new representation generator
//
// The provided store must be non-nil.  A nil store would otherwise lead to a
// nil-pointer panic later when methods like GenerateRawText are invoked.  By
// validating up-front we fail fast with a clear message helping callers
// diagnose the issue.
func NewRepresentationGenerator(store model.RepresentationStore) *RepresentationGenerator {
	if store == nil {
		// Mention the concrete interface type so callers can more easily
		// correlate the panic with the constructor signature.  The previous
		// message simply said “nil representationStore” which is vague when
		// reading from code; by spelling out model.RepresentationStore the
		// panic makes the required parameter clearer.
		panic("NewRepresentationGenerator: nil model.RepresentationStore provided")
	}
	return &RepresentationGenerator{store: store}
}

// GenerateRawText creates a raw_text representation for text-based documents.
// It reads the file content, normalizes to UTF-8, and stores it as a representation.
//
// According to SPEC §7.4:
// - For code/text/md/data/html doc types
// - Normalize to UTF-8 with \n line endings
// - Route code → index_kind=code, others → index_kind=text
// GenerateRawText creates a raw_text representation for text-based documents.
// It reads the file content, normalizes to UTF-8, and stores it as a representation.
//
// According to SPEC §7.4:
// - For code/text/md/data/html doc types
// - Normalize to UTF-8 with \n line endings
// - Route code → index_kind=code, others → index_kind=text
func (rg *RepresentationGenerator) GenerateRawText(ctx context.Context, doc model.Document, absPath string) error {
	// Read file content first so we can delegate to the new helper which
	// accepts pre-loaded bytes.  This keeps the original behaviour intact
	// while allowing callers that already have the content to avoid the I/O.
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", doc.RelPath, err)
	}
	return rg.GenerateRawTextFromContent(ctx, doc, content)
}

// GenerateRawTextFromContent behaves like GenerateRawText but takes the
// document bytes as an argument.  This is useful when the caller already
// loaded the file (e.g. during a scan) and wants to avoid re-reading it.
// The absolute path is no longer required; callers that previously had it
// simply read the file to supply the content.  Removing the parameter
// simplifies the API and avoids unused variable warnings.
func (rg *RepresentationGenerator) GenerateRawTextFromContent(ctx context.Context, doc model.Document, content []byte) error {
	// Guard against huge files to avoid OOM.  We mirror the same limit used by
	// discovery since raw-text ingestion should follow the same policy.
	if int64(len(content)) > defaultMaxFileSizeBytes {
		return fmt.Errorf("file %s too large (%d bytes); limit %d", doc.RelPath, len(content), defaultMaxFileSizeBytes)
	}

	// Validate and normalize UTF-8
	normalizedContent := normalizeUTF8(content)

	// Compute representation hash
	repHash := computeRepHash(normalizedContent)

	// Create representation
	rep := model.Representation{
		DocID:       doc.DocID,
		RepType:     RepTypeRawText,
		RepHash:     repHash,
		CreatedUnix: time.Now().Unix(),
		Deleted:     false,
	}

	// Store representation
	repID, err := rg.store.UpsertRepresentation(ctx, rep)
	if err != nil {
		return fmt.Errorf("upsert representation: %w", err)
	}

	segments := chunkRawTextByDocType(doc.DocType, string(normalizedContent))
	if err := rg.upsertChunksForRepresentation(ctx, repID, indexKindForDocType(doc.DocType), segments); err != nil {
		return err
	}

	return nil
}
func (rg *RepresentationGenerator) upsertChunksForRepresentation(ctx context.Context, repID int64, indexKind string, segments []chunkSegment) error {
	// wrap the entire operation in a transaction so we don't end up with a
	// partial set of chunks if an insertion fails halfway through.  The store
	// implementation handles beginning/committing/rolling back the tx.
	return rg.store.WithTx(ctx, func(tx model.RepresentationStore) error {
		for i, seg := range segments {
			chunk := model.Chunk{
				RepID:           repID,
				Ordinal:         i,
				Text:            seg.Text,
				TextHash:        computeRepHash([]byte(seg.Text)),
				IndexKind:       indexKind,
				EmbeddingStatus: "pending",
			}
			if _, err := tx.InsertChunkWithSpans(ctx, chunk, []model.Span{seg.Span}); err != nil {
				return fmt.Errorf("insert chunk %d: %w", i, err)
			}
		}
		if err := tx.SoftDeleteChunksFromOrdinal(ctx, repID, len(segments)); err != nil {
			return fmt.Errorf("soft delete stale chunks: %w", err)
		}
		return nil
	})
}

// normalizeUTF8 ensures content is valid UTF-8 and normalizes line endings to \n
// Invalid byte sequences are replaced with the Unicode replacement character
// and the resulting slice is returned.  The previous signature returned an
// error that was never produced; simplifying to a single return value makes
// callers easier to work with.
func normalizeUTF8(content []byte) []byte {
	// Salvage any invalid UTF-8 by replacing with U+FFFD.
	if !utf8.Valid(content) {
		out := strings.ToValidUTF8(string(content), "\uFFFD")
		content = []byte(out)
	}

	// Normalize line endings: convert \r\n and \r to \n
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))

	return content
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

type chunkSegment struct {
	Text string
	Span model.Span
}

func indexKindForDocType(docType string) string {
	if docType == "code" {
		return "code"
	}
	return "text"
}

func chunkRawTextByDocType(docType, content string) []chunkSegment {
	if docType == "code" {
		return chunkCodeByLines(content, 200, 30)
	}
	return chunkTextByChars(content, 2500, 250, 200)
}

func chunkOCRByPages(content string) []chunkSegment {
	pages := strings.Split(content, "\f")
	out := make([]chunkSegment, 0, len(pages))
	for i, page := range pages {
		page = strings.TrimSpace(page)
		if page == "" {
			continue
		}
		out = append(out, chunkSegment{
			Text: page,
			Span: model.Span{
				Kind: "page",
				Page: i + 1,
			},
		})
	}
	return out
}

func chunkCodeByLines(content string, maxLines, overlapLines int) []chunkSegment {
	if maxLines <= 0 {
		maxLines = 200
	}
	if overlapLines < 0 {
		overlapLines = 0
	}
	if overlapLines >= maxLines {
		overlapLines = maxLines - 1
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return nil
	}

	step := maxLines - overlapLines
	if step <= 0 {
		step = 1
	}

	out := make([]chunkSegment, 0, (len(lines)/step)+1)
	for start := 0; start < len(lines); start += step {
		end := start + maxLines
		if end > len(lines) {
			end = len(lines)
		}
		if start >= end {
			break
		}
		text := strings.Join(lines[start:end], "\n")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, chunkSegment{
			Text: text,
			Span: model.Span{
				Kind:      "lines",
				StartLine: start + 1,
				EndLine:   end,
			},
		})
		if end == len(lines) {
			break
		}
	}
	return out
}

func chunkTextByChars(content string, maxChars, overlapChars, minChars int) []chunkSegment {
	if maxChars <= 0 {
		maxChars = 2500
	}
	if overlapChars < 0 {
		overlapChars = 0
	}
	if overlapChars >= maxChars {
		overlapChars = maxChars - 1
	}
	if minChars <= 0 {
		minChars = 1
	}

	runes := []rune(content)
	if len(runes) == 0 {
		return nil
	}

	step := maxChars - overlapChars
	if step <= 0 {
		step = 1
	}

	// Precompute line starts (rune offsets) for line-span mapping.
	lineStarts := []int{0}
	for i, r := range runes {
		if r == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}

	out := make([]chunkSegment, 0, (len(runes)/step)+1)
	for start := 0; start < len(runes); start += step {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		if start >= end {
			break
		}

		segmentRunes := runes[start:end]
		segmentText := strings.TrimSpace(string(segmentRunes))
		if len([]rune(segmentText)) < minChars && end != len(runes) {
			continue
		}
		if segmentText == "" {
			continue
		}

		startLine := lineNumberForOffset(lineStarts, start)
		endLine := lineNumberForOffset(lineStarts, end-1)
		out = append(out, chunkSegment{
			Text: segmentText,
			Span: model.Span{
				Kind:      "lines",
				StartLine: startLine,
				EndLine:   endLine,
			},
		})
		if end == len(runes) {
			break
		}
	}
	return out
}

func lineNumberForOffset(lineStarts []int, offset int) int {
	// Keep original edge-case behavior
	if offset <= 0 {
		return 1
	}
	// Locate first index where lineStarts[i] > offset using binary search.
	// The desired line number is the index of the greatest entry <= offset,
	// which corresponds to the returned index from Search (first > offset).
	idx := sort.Search(len(lineStarts), func(i int) bool {
		return lineStarts[i] > offset
	})
	if idx == 0 {
		// offset is less than or equal to the first entry; return first line
		return 1
	}
	// idx is the first index with a start greater than offset; the line is idx
	return idx
}
