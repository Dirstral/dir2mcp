package model

import (
	"context"
	"errors"
)

// Common errors
var (
	ErrNotFound        = errors.New("not found")
	ErrNotImplemented  = errors.New("not implemented")
)

// Document represents a file or archive member in the index
type Document struct {
	DocID       int64  // Primary key
	RelPath     string // Relative path from root (normalized with /)
	SourceType  string // "file" or "archive_member"
	DocType     string // code, text, md, pdf, image, audio, data, html, archive, binary_ignored
	SizeBytes   int64
	MTimeUnix   int64
	ContentHash string // sha256 hash of file content
	Status      string // ok, skipped, error, secret_excluded
	Error       string // Error message if status is error
	Deleted     bool   // Tombstone for deleted documents
}

// Representation is a text view derived from a document
type Representation struct {
	RepID       int64  // Primary key
	DocID       int64  // Foreign key to documents
	RepType     string // raw_text, ocr_markdown, transcript, annotation_json, annotation_text
	RepHash     string // sha256 hash of representation content
	CreatedUnix int64
	MetaJSON    string // JSON metadata (provider, model, timestamps, etc.)
	Deleted     bool   // Tombstone
}

// Chunk represents a span of a representation used for embedding and retrieval
type Chunk struct {
	ChunkID         int64  // Primary key; also used as ANN label
	RepID           int64  // Foreign key to representations
	Ordinal         int    // Position within representation
	Text            string // Chunk text content
	TextHash        string // Hash of text for incremental updates
	TokensEst       int    // Estimated token count
	IndexKind       string // "text" or "code" - determines which HNSW index
	EmbeddingStatus string // "ok", "pending", "error"
	EmbeddingError  string // Error message if embedding failed
	Deleted         bool   // Tombstone
}

// Span represents provenance coordinates for citations
type Span struct {
	ChunkID   int64  // Foreign key to chunks
	SpanKind  string // "lines", "page", or "time"
	Start     int    // start_line, page number, or start_ms
	End       int    // end_line, page number, or end_ms
	ExtraJSON string // Optional additional metadata
}

// ChunkMetadata holds metadata about a chunk for the embedding worker
type ChunkMetadata struct {
	ChunkID  int64
	RepID    int64
	DocID    int64
	RelPath  string
	DocType  string
	RepType  string
	Ordinal  int
}

// ChunkTask represents a chunk that needs embedding
type ChunkTask struct {
	Label    int64         // Deprecated: use Metadata.ChunkID instead
	Text     string        // Text to embed
	Metadata ChunkMetadata // Full metadata
}

// Validate checks if a ChunkTask is valid
func (ct *ChunkTask) Validate() error {
	if ct.Metadata.ChunkID < 0 {
		return errors.New("chunk_id must be non-negative")
	}
	if ct.Label != ct.Metadata.ChunkID {
		return errors.New("label must match metadata.chunk_id")
	}
	if ct.Text == "" {
		return errors.New("text cannot be empty")
	}
	return nil
}

// Store is the interface for metadata storage operations
type Store interface {
	// Document operations
	UpsertDocument(ctx context.Context, doc Document) error
	GetDocumentByPath(ctx context.Context, relPath string) (Document, error)
	ListFiles(ctx context.Context, pathPrefix string, glob string, limit int, offset int) ([]Document, int64, error)
	
	// Representation operations
	UpsertRepresentation(ctx context.Context, rep Representation, content []byte) error
	ListRepresentations(ctx context.Context, docID int64) ([]Representation, error)
	GetRepresentation(ctx context.Context, repID int64) (Representation, error)
	
	// Chunk operations
	UpsertChunk(ctx context.Context, chunk Chunk) error
	ListChunks(ctx context.Context, repID int64) ([]Chunk, error)
	
	// Span operations
	UpsertSpan(ctx context.Context, span Span) error
	ListSpans(ctx context.Context, chunkID int64) ([]Span, error)
}

// Index is the interface for ANN vector index operations
type Index interface {
	// Add adds a vector with the given label
	Add(label uint64, vector []float32) error
	
	// Search finds the k nearest neighbors
	Search(vector []float32, k int) ([]uint64, []float32, error)
	
	// Delete removes a vector (may not be supported by all implementations)
	Delete(label uint64) error
	
	// Save persists the index to disk
	Save(path string) error
	
	// Load loads the index from disk
	Load(path string) error
}

// Embedder is the interface for generating embeddings
type Embedder interface {
	// Embed generates embeddings for a batch of texts
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// Retriever is the interface for search and retrieval operations
type Retriever interface {
	// Search performs vector search and returns ranked results
	Search(ctx context.Context, query string, k int, filters map[string]interface{}) ([]SearchHit, error)
	
	// Ask performs RAG-based question answering
	Ask(ctx context.Context, question string, k int, filters map[string]interface{}) (Answer, error)
}

// SearchHit represents a single search result
type SearchHit struct {
	ChunkID  int64
	RelPath  string
	RepType  string
	Score    float32
	Snippet  string
	Span     *Span
}

// Answer represents a RAG answer
type Answer struct {
	Text      string
	Citations []Citation
	Hits      []SearchHit
}

// Citation represents a citation in an answer
type Citation struct {
	RelPath   string
	SpanKind  string
	Start     int
	End       int
	Text      string
}
