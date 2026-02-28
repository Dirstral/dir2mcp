package cli

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/model"
)

type mutableCorpusStore struct {
	mu   sync.RWMutex
	docs []model.Document
}

func (m *mutableCorpusStore) Init(context.Context) error { return nil }
func (m *mutableCorpusStore) UpsertDocument(context.Context, model.Document) error {
	return nil
}
func (m *mutableCorpusStore) GetDocumentByPath(context.Context, string) (model.Document, error) {
	return model.Document{}, model.ErrNotImplemented
}
func (m *mutableCorpusStore) Close() error { return nil }

func (m *mutableCorpusStore) ListFiles(_ context.Context, _ string, _ string, limit, offset int) ([]model.Document, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if offset >= len(m.docs) {
		return []model.Document{}, int64(len(m.docs)), nil
	}
	end := offset + limit
	if end > len(m.docs) {
		end = len(m.docs)
	}
	return append([]model.Document(nil), m.docs[offset:end]...), int64(len(m.docs)), nil
}

func (m *mutableCorpusStore) setDocs(docs []model.Document) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs = append([]model.Document(nil), docs...)
}

func TestRunCorpusWriterWithInterval_UpdatesSnapshotWhileRunning(t *testing.T) {
	stateDir := t.TempDir()
	store := &mutableCorpusStore{}
	store.setDocs([]model.Document{
		{RelPath: "src/a.go", DocType: "code"},
		{RelPath: "docs/a.md", DocType: "md"},
	})

	idxState := appstate.NewIndexingState(appstate.ModeIncremental)
	idxState.SetRunning(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runCorpusWriterWithInterval(ctx, stateDir, store, idxState, io.Discard, 20*time.Millisecond)

	corpusPath := filepath.Join(stateDir, "corpus.json")
	waitForCondition(t, 2*time.Second, func() bool {
		_, err := os.Stat(corpusPath)
		return err == nil
	})

	initial := readCorpusFile(t, corpusPath)
	if initial.TotalDocs != 2 {
		t.Fatalf("expected initial total_docs=2, got %d", initial.TotalDocs)
	}
	eps := 1e-3
	if math.Abs(initial.CodeRatio-0.5) > eps {
		t.Fatalf("expected initial code_ratio around 0.5 (±%f), got %f", eps, initial.CodeRatio)
	}

	store.setDocs([]model.Document{
		{RelPath: "src/a.go", DocType: "code"},
		{RelPath: "docs/a.md", DocType: "md"},
		{RelPath: "docs/b.md", DocType: "md"},
	})

	waitForCondition(t, 2*time.Second, func() bool {
		updated, err := readCorpusFileMaybe(t, corpusPath)
		if err != nil {
			// ignore transient read/unmarshal errors during partial writes
			return false
		}
		return updated.TotalDocs == 3 && updated.DocCounts["md"] == 2
	})
	updated := readCorpusFile(t, corpusPath)
	if math.Abs(updated.CodeRatio-0.3333) > eps {
		t.Fatalf("expected updated code_ratio around 0.3333 (±%f), got %f", eps, updated.CodeRatio)
	}
}

func TestWriteCorpusSnapshot_ConcurrentWriters(t *testing.T) {
	stateDir := t.TempDir()
	store := &mutableCorpusStore{}
	store.setDocs([]model.Document{
		{RelPath: "src/a.go", DocType: "code"},
		{RelPath: "docs/a.md", DocType: "md"},
	})
	idxState := appstate.NewIndexingState(appstate.ModeIncremental)

	const writers = 16
	const writesPerWriter = 20

	var wg sync.WaitGroup
	errCh := make(chan error, writers*writesPerWriter)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				if err := writeCorpusSnapshot(context.Background(), stateDir, store, idxState); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("writeCorpusSnapshot failed under concurrent writes: %v", err)
	}

	// Ensure final corpus snapshot is valid JSON and has expected fields.
	final := readCorpusFile(t, filepath.Join(stateDir, "corpus.json"))
	if final.TotalDocs != 2 {
		t.Fatalf("expected total_docs=2, got %d", final.TotalDocs)
	}
	if final.DocCounts["code"] != 1 || final.DocCounts["md"] != 1 {
		t.Fatalf("unexpected doc counts: %#v", final.DocCounts)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	if fn() {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Duration(10+rand.Intn(11)) * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func readCorpusFile(t *testing.T, path string) corpusSnapshot {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus file: %v", err)
	}
	var snap corpusSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal corpus file: %v", err)
	}
	return snap
}

func readCorpusFileMaybe(t *testing.T, path string) (corpusSnapshot, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return corpusSnapshot{}, err
	}
	var snap corpusSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return corpusSnapshot{}, err
	}
	return snap, nil
}
