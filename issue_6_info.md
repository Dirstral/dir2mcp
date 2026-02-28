# GitHub Issue #6

## Summary

1. **Hash-based change detection** for incremental indexing
2. **raw_text representation generation** for text-based documents

## What Was Implemented

### Part 1: Hash-based Change Detection

**File: `hash.go`**

Implements hash-based change detection to enable incremental indexing:

- `computeContentHash()`: Computes SHA256 hash of file content for document-level change detection
- `computeRepHash()`: Computes SHA256 hash of representation content for rep-level change detection
- `needsReprocessing()`: Determines if a document needs reprocessing based on hash comparison

**Changes to `ingest_service.go`:**

- Modified `runScan()` to support incremental vs. full reindex modes
- Added `processDocument()` method that:
  1. Builds document and computes content hash
  2. Retrieves existing document from store (if it exists)
  3. Compares old hash vs. new hash
  4. Skips representation generation if hash unchanged (incremental mode)
  5. Reprocesses everything if hash changed or in full reindex mode
- Split `buildDocument()` into `buildDocumentWithContent()` to avoid reading files twice
- Added `generateRepresentations()` method to orchestrate representation creation
- Added `addRepresentations()` counter tracking

### Part 2: raw_text Representation

**File: `represent.go`**

Implements raw_text representation generation for text-based documents:

- `RepresentationGenerator`: Handles creation of representations from documents
- `GenerateRawText()`: Creates raw_text representation with:
  - UTF-8 validation and normalization
  - Line ending normalization (convert `\r\n` and `\r` to `\n`)
  - Representation hash computation
  - Incremental update check (skip if unchanged)
  - Metadata JSON creation
  - Store upsert
- `ShouldGenerateRawText()`: Determines which doc types should generate raw_text
  - Supports: code, text, md, data, html (per SPEC §7.4)

**Constants defined:**
- `RepTypeRawText` = "raw_text"
- `RepTypeOCRMarkdown` = "ocr_markdown" (for future use)
- `RepTypeTranscript` = "transcript" (for future use)
- `RepTypeAnnotationJSON` = "annotation_json" (for future use)
- `RepTypeAnnotationText` = "annotation_text" (for future use)

### Supporting Infrastructure

**File: `utilities.go`**

Helper functions for document processing:

- `ClassifyDocType()`: Classifies documents into types (code, text, md, pdf, image, audio, data, html, archive, binary_ignored)
- `compileSecretPatterns()`: Compiles regex patterns for secret detection
- `hasSecretMatch()`: Checks if content matches any secret patterns
- `matchesAnyPathExclude()`: Glob pattern matching for path exclusions
- `matchesGlobPattern()`: Supports `*` and `**` wildcards

**File: `model.go`**

Interface definitions and data types:

- `Document`: File metadata structure
- `Representation`: Text view derived from documents
- `Chunk`: Span of representation for embedding (placeholder for part 3)
- `Span`: Provenance coordinates for citations
- `Store`: Interface for metadata storage operations
  - `GetDocumentByPath()`: Retrieve document for hash comparison
  - `UpsertDocument()`: Insert or update document
  - `UpsertRepresentation()`: Insert or update representation with content
  - `ListRepresentations()`: List representations for a document
- Error constants: `ErrNotFound`, `ErrNotImplemented`

**File: `appstate.go`**

Indexing state tracking:

- `IndexingState`: Thread-safe state tracker with atomic counters
- `IndexingMode`: Enum for "incremental" vs "full" reindex
- Counters: scanned, indexed, skipped, deleted, representations, chunks_total, embedded, errors
- `Snapshot()`: Captures point-in-time state for reporting

## How It Works

### Incremental Indexing Flow

```
1. Scan directory → discover files
2. For each discovered file:
   a. Compute content hash (SHA256)
   b. Look up existing document in store by rel_path
   c. Compare old hash vs new hash
   d. If unchanged → skip representation generation
   e. If changed or new → generate representations
3. Mark missing files as deleted
```

### Full Reindex Flow

```
1. Set mode to ModeFull
2. Scan directory → discover files
3. For each discovered file:
   a. Compute content hash (SHA256)
   b. Always generate representations (ignore hash comparison)
4. Mark missing files as deleted
```

### raw_text Generation Flow

```
1. Check if doc type should have raw_text (code, text, md, data, html)
2. If no → skip
3. If yes:
   a. Read file content
   b. Normalize UTF-8
   c. Normalize line endings (\r\n, \r → \n)
   d. Compute rep hash
   e. Check if representation exists with same hash
   f. If unchanged → skip
   g. If changed or new → upsert to store with metadata
```

## Integration Points

### Required Store Interface Methods

Your `Store` implementation must provide:

```go
type Store interface {
    // For hash-based change detection
    GetDocumentByPath(ctx context.Context, relPath string) (Document, error)
    
    // For document upsert
    UpsertDocument(ctx context.Context, doc Document) error
    
    // For representation generation
    UpsertRepresentation(ctx context.Context, rep Representation, content []byte) error
    ListRepresentations(ctx context.Context, docID int64) ([]Representation, error)
    
    // For marking deletions
    MarkDocumentDeleted(ctx context.Context, relPath string) error // Optional interface
    
    // For listing (existing)
    ListFiles(ctx context.Context, pathPrefix string, glob string, limit int, offset int) ([]Document, int64, error)
}
```

### Error Handling

The implementation expects:
- `model.ErrNotFound` when a document doesn't exist (for incremental indexing)
- `model.ErrNotImplemented` when a store method isn't implemented (gracefully skipped)

## Configuration

Uses existing `config.Config` fields:
- `RootDir`: Directory to index
- `PathExcludes`: Glob patterns for excluded paths
- `SecretPatterns`: Regex patterns for secret detection

## File Structure

```
internal/ingest/
├── hash.go              # Hash-based change detection
├── represent.go         # Representation generation (raw_text)
├── utilities.go         # Document classification, secret detection, path exclusions
├── ingest_service.go    # Main ingestion pipeline (updated)
├── discover.go          # File discovery (existing)
└── ...

internal/model/
└── model.go             # Data types and interfaces

internal/appstate/
└── appstate.go          # Indexing state tracking

internal/config/
└── config.go            # Configuration (existing)
```