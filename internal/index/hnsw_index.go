package index

import (
	"encoding/gob"
	"errors"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
)

type HNSWIndex struct {
	path    string
	mu      sync.RWMutex
	vectors map[uint64][]float32

	// Logger is optional; if non-nil its Printf method will be used for
	// informational messages. When nil the standard library's log package
	// is used.
	Logger *log.Logger

	// Metrics collects optional counters that callers can inspect. The
	// zero-value of HNSWIndexMetrics is usable, so callers may simply pass a
	// pointer and read it after operations. If Metrics is nil nothing is
	// incremented.
	Metrics *HNSWIndexMetrics
}

// HNSWIndexMetrics holds counters gathered by an index instance.
//
// Additional fields may be added in future if callers require them.
// Only the dimension mismatch counter is currently defined.
type HNSWIndexMetrics struct {
	// DimensionMismatch tracks how many times a provided query vector
	// didn't match the length of a stored vector.  We use atomic.Int64
	// instead of a plain int64 so that metrics can be read concurrently
	// on 32‑bit architectures without data races.
	DimensionMismatch atomic.Int64
}

// NewHNSWIndex creates an empty in-memory HNSW index. The optional
// path argument is used by Save/Load; if non-empty those methods will
// persist to the given file.
func NewHNSWIndex(path string) *HNSWIndex {
	return &HNSWIndex{
		path:    path,
		vectors: make(map[uint64][]float32),
	}
}

func (i *HNSWIndex) Add(label uint64, vector []float32) error {
	if len(vector) == 0 {
		return errors.New("vector cannot be empty")
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	copied := make([]float32, len(vector))
	copy(copied, vector)
	i.vectors[label] = copied
	return nil
}

func (i *HNSWIndex) Search(vector []float32, k int) ([]uint64, []float32, error) {
	if len(vector) == 0 {
		return nil, nil, errors.New("query vector cannot be empty")
	}
	if k <= 0 {
		return []uint64{}, []float32{}, nil
	}

	type scored struct {
		label uint64
		score float32
	}

	// We only hold the read lock long enough to inspect each vector's
	// length, bump the metric on mismatch, and copy candidates for later
	// scoring.
	// define local types for readability
	type mismatch struct {
		label    uint64
		candLen  int
		queryLen int
	}
	type candidate struct {
		label  uint64
		vector []float32
	}
	var (
		mismatches []mismatch
		candidates []candidate
	)

	i.mu.RLock()
	// collect matching candidates while holding the lock
	for label, cand := range i.vectors {
		if len(cand) != len(vector) {
			mismatches = append(mismatches, mismatch{label, len(cand), len(vector)})
			if i.Metrics != nil {
				// atomic counter; update under lock to keep close to observation
				i.Metrics.DimensionMismatch.Add(1)
			}
			continue
		}
		copyVec := make([]float32, len(cand))
		copy(copyVec, cand)
		candidates = append(candidates, candidate{label, copyVec})
	}
	i.mu.RUnlock()

	// perform logging outside the lock to avoid blocking other routines
	for _, m := range mismatches {
		i.logf("dimension mismatch: label=%d candidate_len=%d query_len=%d", m.label, m.candLen, m.queryLen)
	}

	// now that the lock is released, compute similarities
	scoredItems := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		scoredItems = append(scoredItems, scored{
			label: c.label,
			score: cosineSimilarity(vector, c.vector),
		})
	}

	sort.Slice(scoredItems, func(a, b int) bool {
		if scoredItems[a].score == scoredItems[b].score {
			return scoredItems[a].label < scoredItems[b].label
		}
		return scoredItems[a].score > scoredItems[b].score
	})

	if len(scoredItems) > k {
		scoredItems = scoredItems[:k]
	}

	labels := make([]uint64, len(scoredItems))
	scores := make([]float32, len(scoredItems))
	for idx, item := range scoredItems {
		labels[idx] = item.label
		scores[idx] = item.score
	}

	return labels, scores, nil
}

// logf is a small helper that routes messages to the configured logger or
// the global log package. It mirrors the helper defined on EmbeddingWorker.
func (i *HNSWIndex) logf(format string, args ...interface{}) {
	if i != nil && i.Logger != nil {
		i.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (i *HNSWIndex) Save(path string) error {
	if path == "" {
		path = i.path
	}
	if path == "" {
		return errors.New("path is required")
	}

	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	i.mu.RLock()
	defer i.mu.RUnlock()
	enc := gob.NewEncoder(file)
	err = enc.Encode(i.vectors)
	if err != nil {
		closeErr := file.Close()
		_ = os.Remove(tmpPath)
		return errors.Join(err, closeErr)
	}
	if err := file.Sync(); err != nil {
		closeErr := file.Close()
		_ = os.Remove(tmpPath)
		return errors.Join(err, closeErr)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (i *HNSWIndex) Load(path string) error {
	if path == "" {
		path = i.path
	}
	if path == "" {
		return errors.New("path is required")
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = file.Close() }()

	loaded := make(map[uint64][]float32)
	dec := gob.NewDecoder(file)
	if err := dec.Decode(&loaded); err != nil {
		return err
	}

	i.mu.Lock()
	i.vectors = loaded
	i.mu.Unlock()
	return nil
}

func (i *HNSWIndex) Close() error {
	return nil
}

func cosineSimilarity(a, b []float32) float32 {
	var dot float32
	var magA float32
	var magB float32

	for idx := range a {
		dot += a[idx] * b[idx]
		magA += a[idx] * a[idx]
		magB += b[idx] * b[idx]
	}

	if magA == 0 || magB == 0 {
		return 0
	}

	return dot / sqrt32(magA*magB)
}

func sqrt32(v float32) float32 {
	// use standard library math for correctness and simplicity; casting
	// handles 0 implicitly.
	return float32(math.Sqrt(float64(v)))
}
