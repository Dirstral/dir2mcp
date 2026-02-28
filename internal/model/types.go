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
