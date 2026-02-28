package index

import (
	"encoding/gob"
	"errors"
	"os"
	"sort"
	"sync"
)

type HNSWIndex struct {
	path    string
	mu      sync.RWMutex
	vectors map[uint64][]float32
}

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

	i.mu.RLock()
	defer i.mu.RUnlock()

	scoredItems := make([]scored, 0, len(i.vectors))
	for label, candidate := range i.vectors {
		if len(candidate) != len(vector) {
			continue
		}
		scoredItems = append(scoredItems, scored{
			label: label,
			score: cosineSimilarity(vector, candidate),
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

func (i *HNSWIndex) Save(path string) error {
	if path == "" {
		path = i.path
	}
	if path == "" {
		return errors.New("path is required")
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	i.mu.RLock()
	defer i.mu.RUnlock()

	enc := gob.NewEncoder(file)
	return enc.Encode(i.vectors)
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
		if os.IsNotExist(err) {
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
	// Newton-Raphson iteration is sufficient for ranking comparisons.
	x := v
	if x == 0 {
		return 0
	}
	for iter := 0; iter < 8; iter++ {
		x = 0.5 * (x + v/x)
	}
	return x
}
