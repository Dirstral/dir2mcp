package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHNSWIndex_AddAndSearch(t *testing.T) {
	idx := NewHNSWIndex("")
	if err := idx.Add(1, []float32{1, 0}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := idx.Add(2, []float32{0.9, 0.1}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := idx.Add(3, []float32{0, 1}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	labels, scores, err := idx.Search([]float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(labels) != 2 || len(scores) != 2 {
		t.Fatalf("unexpected result lengths: labels=%d scores=%d", len(labels), len(scores))
	}
	if labels[0] != 1 {
		t.Fatalf("expected top label 1, got %d", labels[0])
	}
}

func TestHNSWIndex_SaveAndLoad(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "idx.bin")

	idx := NewHNSWIndex(file)
	if err := idx.Add(7, []float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := idx.Save(""); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("expected saved file: %v", err)
	}

	loaded := NewHNSWIndex(file)
	if err := loaded.Load(""); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	labels, _, err := loaded.Search([]float32{0.1, 0.2, 0.3}, 1)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(labels) != 1 || labels[0] != 7 {
		t.Fatalf("unexpected loaded search result: %#v", labels)
	}
}
