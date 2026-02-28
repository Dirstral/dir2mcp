package appstate

import (
	"sync/atomic"
)

// IndexingMode represents the mode of indexing operation
type IndexingMode string

const (
	// ModeIncremental performs incremental indexing (skip unchanged files)
	ModeIncremental IndexingMode = "incremental"
	
	// ModeFull performs full reindexing (reprocess all files)
	ModeFull IndexingMode = "full"
)

// IndexingState tracks the progress of indexing operations.
// All counters use atomic operations for thread-safe updates.
type IndexingState struct {
	mode           atomic.Value // IndexingMode
	running        atomic.Bool
	scanned        atomic.Int64
	indexed        atomic.Int64
	skipped        atomic.Int64
	deleted        atomic.Int64
	representations atomic.Int64
	chunksTotal    atomic.Int64
	embedded       atomic.Int64
	errors         atomic.Int64
}

// NewIndexingState creates a new indexing state tracker
func NewIndexingState() *IndexingState {
	state := &IndexingState{}
	state.mode.Store(ModeIncremental)
	return state
}

// SetMode sets the indexing mode
func (s *IndexingState) SetMode(mode IndexingMode) {
	s.mode.Store(mode)
}

// GetMode returns the current indexing mode
func (s *IndexingState) GetMode() IndexingMode {
	return s.mode.Load().(IndexingMode)
}

// SetRunning sets whether indexing is currently running
func (s *IndexingState) SetRunning(running bool) {
	s.running.Store(running)
}

// IsRunning returns whether indexing is currently running
func (s *IndexingState) IsRunning() bool {
	return s.running.Load()
}

// AddScanned increments the scanned counter
func (s *IndexingState) AddScanned(delta int64) {
	s.scanned.Add(delta)
}

// GetScanned returns the current scanned count
func (s *IndexingState) GetScanned() int64 {
	return s.scanned.Load()
}

// AddIndexed increments the indexed counter
func (s *IndexingState) AddIndexed(delta int64) {
	s.indexed.Add(delta)
}

// GetIndexed returns the current indexed count
func (s *IndexingState) GetIndexed() int64 {
	return s.indexed.Load()
}

// AddSkipped increments the skipped counter
func (s *IndexingState) AddSkipped(delta int64) {
	s.skipped.Add(delta)
}

// GetSkipped returns the current skipped count
func (s *IndexingState) GetSkipped() int64 {
	return s.skipped.Load()
}

// AddDeleted increments the deleted counter
func (s *IndexingState) AddDeleted(delta int64) {
	s.deleted.Add(delta)
}

// GetDeleted returns the current deleted count
func (s *IndexingState) GetDeleted() int64 {
	return s.deleted.Load()
}

// AddRepresentations increments the representations counter
func (s *IndexingState) AddRepresentations(delta int64) {
	s.representations.Add(delta)
}

// GetRepresentations returns the current representations count
func (s *IndexingState) GetRepresentations() int64 {
	return s.representations.Load()
}

// AddChunksTotal increments the chunks total counter
func (s *IndexingState) AddChunksTotal(delta int64) {
	s.chunksTotal.Add(delta)
}

// GetChunksTotal returns the current chunks total count
func (s *IndexingState) GetChunksTotal() int64 {
	return s.chunksTotal.Load()
}

// AddEmbedded increments the embedded counter
func (s *IndexingState) AddEmbedded(delta int64) {
	s.embedded.Add(delta)
}

// GetEmbedded returns the current embedded count
func (s *IndexingState) GetEmbedded() int64 {
	return s.embedded.Load()
}

// AddErrors increments the errors counter
func (s *IndexingState) AddErrors(delta int64) {
	s.errors.Add(delta)
}

// GetErrors returns the current errors count
func (s *IndexingState) GetErrors() int64 {
	return s.errors.Load()
}

// Reset resets all counters to zero
func (s *IndexingState) Reset() {
	s.scanned.Store(0)
	s.indexed.Store(0)
	s.skipped.Store(0)
	s.deleted.Store(0)
	s.representations.Store(0)
	s.chunksTotal.Store(0)
	s.embedded.Store(0)
	s.errors.Store(0)
}

// Snapshot returns a snapshot of the current state
func (s *IndexingState) Snapshot() Snapshot {
	return Snapshot{
		Mode:            s.GetMode(),
		Running:         s.IsRunning(),
		Scanned:         s.GetScanned(),
		Indexed:         s.GetIndexed(),
		Skipped:         s.GetSkipped(),
		Deleted:         s.GetDeleted(),
		Representations: s.GetRepresentations(),
		ChunksTotal:     s.GetChunksTotal(),
		Embedded:        s.GetEmbedded(),
		Errors:          s.GetErrors(),
	}
}

// Snapshot represents a point-in-time snapshot of indexing state
type Snapshot struct {
	Mode            IndexingMode
	Running         bool
	Scanned         int64
	Indexed         int64
	Skipped         int64
	Deleted         int64
	Representations int64
	ChunksTotal     int64
	Embedded        int64
	Errors          int64
}
