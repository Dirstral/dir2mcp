package index

import "github.com/Dirstral/dir2mcp/internal/model"

type HNSWIndex struct {
	path string
}

func NewHNSWIndex(path string) *HNSWIndex {
	return &HNSWIndex{path: path}
}

func (i *HNSWIndex) Add(label uint64, vector []float32) error {
	_ = label
	_ = vector
	return model.ErrNotImplemented
}

func (i *HNSWIndex) Search(vector []float32, k int) ([]uint64, []float32, error) {
	_ = vector
	_ = k
	return nil, nil, model.ErrNotImplemented
}

func (i *HNSWIndex) Save(path string) error {
	_ = path
	return model.ErrNotImplemented
}

func (i *HNSWIndex) Load(path string) error {
	_ = path
	return model.ErrNotImplemented
}

func (i *HNSWIndex) Close() error {
	return nil
}
