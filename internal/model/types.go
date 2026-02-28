package model

import "fmt"

type Document struct {
	DocID       int64
	RelPath     string
	DocType     string
	SizeBytes   int64
	MTimeUnix   int64
	ContentHash string
	Status      string
	Deleted     bool
}

type Representation struct {
	RepID       int64
	DocID       int64
	RepType     string
	RepHash     string
	CreatedUnix int64
	Deleted     bool
}

type Chunk struct {
	ChunkID         uint64
	RepID           int64
	Ordinal         int
	Text            string
	TextHash        string
	IndexKind       string
	EmbeddingStatus string
	EmbeddingError  string
	Deleted         bool
}

type Span struct {
	Kind      string
	StartLine int
	EndLine   int
	Page      int
	StartMS   int
	EndMS     int
}

type SearchQuery struct {
	Query      string
	K          int
	Index      string
	PathPrefix string
	FileGlob   string
	DocTypes   []string
}

type SearchHit struct {
	ChunkID uint64
	RelPath string
	DocType string
	RepType string
	Score   float64
	Snippet string
	Span    Span
}

type ChunkMetadata struct {
	ChunkID uint64
	RelPath string
	DocType string
	RepType string
	Snippet string
	Span    Span
}

// ToSearchHit converts the lightweight chunk metadata back into a full
// SearchHit.  This is convenient when code (e.g. retrieval) still operates on
// SearchHit values but chunk tasks only need a subset of fields.
func (m ChunkMetadata) ToSearchHit() SearchHit {
	return SearchHit{
		ChunkID: m.ChunkID,
		RelPath: m.RelPath,
		DocType: m.DocType,
		RepType: m.RepType,
		Snippet: m.Snippet,
		Span:    m.Span,
	}
}

// ChunkTask represents a pending unit of work that needs an embedding.
//
// Label corresponds to the chunk_id in the SQLite schema and is always a
// positive integer; the type was changed to uint64 for consistency with the
// ANN index which also uses unsigned labels. Metadata is a small subset of
// SearchHit information that is relevant when processing the task
// (the score field is omitted since it isn’t applicable).
//
// Historically the identifier lived only in the Label field; adding
// ChunkMetadata caused duplication and opened the door for the two values to
// diverge. Label is retained for API compatibility with the embedding
// pipeline (EmbeddingWorker, stores etc.) but callers should prefer
// Metadata.ChunkID when they only need an ID. The helper constructors and
// validation method below ensure the two fields remain in sync.

// ChunkTask is intentionally a struct so that callers outside the package may
// construct values in tests or mocks but NewChunkTask should be used by
// production code whenever possible.
type ChunkTask struct {
	Label     uint64
	Text      string
	IndexKind string
	Metadata  ChunkMetadata
}

// NewChunkTask returns a task with the supplied components. If the provided
// metadata already contains a ChunkID it must match the explicit label;
// otherwise the metadata ID is populated. The function panics if the two
// values conflict, which is suitable for use by store code and tests where
// a mismatch indicates a programmer error. Callers that prefer an error
// return can instead construct a value and call Validate.
func NewChunkTask(label uint64, text, indexKind string, meta ChunkMetadata) ChunkTask {
	if meta.ChunkID == 0 {
		meta.ChunkID = label
	} else if label != meta.ChunkID {
		panic(fmt.Sprintf("NewChunkTask: label %d != metadata.ChunkID %d", label, meta.ChunkID))
	}
	return ChunkTask{
		Label:     label,
		Text:      text,
		IndexKind: indexKind,
		Metadata:  meta,
	}
}

// Validate checks that Label and Metadata.ChunkID agree. It returns an error
// if they differ and nil otherwise.
func (t ChunkTask) Validate() error {
	if t.Label != t.Metadata.ChunkID {
		return fmt.Errorf("label %d does not match metadata.chunkID %d", t.Label, t.Metadata.ChunkID)
	}
	return nil
}

type Citation struct {
	ChunkID uint64
	RelPath string
	Span    Span
}

type AskResult struct {
	Question         string
	Answer           string
	Citations        []Citation
	Hits             []SearchHit
	IndexingComplete bool
}

type Stats struct {
	Root            string
	StateDir        string
	ProtocolVersion string
	Scanned         int64
	Indexed         int64
	Skipped         int64
	Deleted         int64
	Representations int64
	ChunksTotal     int64
	EmbeddedOK      int64
	Errors          int64
}
