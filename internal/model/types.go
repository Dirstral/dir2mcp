package model

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
	ChunkID         int64
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
	ChunkID int64
	RelPath string
	DocType string
	RepType string
	Score   float64
	Snippet string
	Span    Span
}

type ChunkMetadata struct {
	ChunkID int64
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
// Label corresponds to the chunk_id in the SQLite schema and follows the
// package convention of using signed int64 identifiers. Metadata is a small
// subset of SearchHit information that is relevant when processing the task
// (the score field is omitted since it isn’t applicable).
type ChunkTask struct {
	Label     int64
	Text      string
	IndexKind string
	Metadata  ChunkMetadata
}

type Citation struct {
	ChunkID int64
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
