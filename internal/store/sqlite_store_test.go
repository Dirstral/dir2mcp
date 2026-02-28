package store

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"dir2mcp/internal/model"
)

func TestSQLiteStore_PendingChunkLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// the schema should have indexes to avoid full table scans on pending
	// lookups and rel_path queries.  verify they were created during Init.
	if err := verifyChunkIndexes(ctx, st); err != nil {
		t.Fatalf("index verification failed: %v", err)
	}

	if err := st.UpsertChunkTask(ctx, model.NewChunkTask(101, "chunk text", "text", model.ChunkMetadata{
		ChunkID: 101,
		RelPath: "docs/a.md",
		DocType: "md",
		RepType: "raw_text",
	})); err != nil {
		t.Fatalf("UpsertChunkTask failed: %v", err)
	}

	tasks, err := st.NextPending(ctx, 10, "text")
	if err != nil {
		t.Fatalf("NextPending failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one task, got %#v", tasks)
	}
	// verify all fields persisted correctly
	expectedText := "chunk text"
	expectedIndexKind := "text"
	expectedMetadata := model.ChunkMetadata{
		ChunkID: 101,
		RelPath: "docs/a.md",
		DocType: "md",
		RepType: "raw_text",
		Snippet: "chunk text",
		Span:    model.Span{Kind: "lines"},
	}
	if tasks[0].Label != 101 || tasks[0].Metadata.ChunkID != 101 ||
		tasks[0].Text != expectedText ||
		tasks[0].IndexKind != expectedIndexKind ||
		!reflect.DeepEqual(tasks[0].Metadata, expectedMetadata) {
		t.Fatalf("unexpected task fields: %#v", tasks)
	}

	if err := st.MarkEmbedded(ctx, []uint64{101}); err != nil {
		t.Fatalf("MarkEmbedded failed: %v", err)
	}
	tasks, err = st.NextPending(ctx, 10, "text")
	if err != nil {
		t.Fatalf("NextPending failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no pending tasks after embedding, got %#v", tasks)
	}
}

func TestSQLiteStore_MarkEmbeddingStatus_LabelOverflow(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// a label that cannot be represented as a signed 64-bit integer
	tooBig := uint64(math.MaxInt64) + 1

	err := st.MarkEmbedded(ctx, []uint64{tooBig})
	if err == nil {
		t.Fatalf("expected error for label > MaxInt64, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("unexpected error message: %v", err)
	}

	err = st.MarkFailed(ctx, []uint64{tooBig}, "reason")
	if err == nil {
		t.Fatalf("expected error for label > MaxInt64 in MarkFailed, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("unexpected error message for MarkFailed: %v", err)
	}
}

func TestSQLiteStore_UpsertChunkTask_RequiresRelPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := st.UpsertChunkTask(ctx, model.NewChunkTask(1, "some text", "text", model.ChunkMetadata{
		ChunkID: 1,
		RelPath: "",
	}))
	if err == nil {
		t.Fatal("expected error for empty RelPath, got nil")
	}

	err = st.UpsertChunkTask(ctx, model.NewChunkTask(2, "some text", "text", model.ChunkMetadata{
		ChunkID: 2,
		RelPath: "   ",
	}))
	if err == nil {
		t.Fatal("expected error for whitespace RelPath, got nil")
	}
}

func TestSQLiteStore_UpsertChunkTask_RequiresNonZeroLabel(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// zero label should be rejected (requires non-zero)
	err := st.UpsertChunkTask(ctx, model.NewChunkTask(0, "text", "text", model.ChunkMetadata{
		ChunkID: 0,
		RelPath: "foo",
	}))
	if err == nil {
		t.Fatal("expected error for label 0, got nil")
	}
	if !strings.Contains(err.Error(), "non-zero") {
		t.Fatalf("unexpected error message for zero label: %v", err)
	}

	// non-zero (positive) label should succeed
	if err := st.UpsertChunkTask(ctx, model.NewChunkTask(1, "text", "text", model.ChunkMetadata{
		ChunkID: 1,
		RelPath: "foo",
	})); err != nil {
		t.Fatalf("expected success for positive label, got %v", err)
	}
}

func TestSQLiteStore_UpsertChunkTask_LabelMetadataMismatch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// construct a task where label and metadata.ChunkID disagree; validation
	// should catch this and return an error instead of panicking or accepting
	// inconsistent data.
	task := model.ChunkTask{
		Label:     1,
		Text:      "mismatch",
		IndexKind: "text",
		Metadata: model.ChunkMetadata{
			ChunkID: 2,
			RelPath: "foo",
		},
	}
	if err := st.UpsertChunkTask(ctx, task); err == nil {
		t.Fatalf("expected error for mismatched label/metadata, got nil")
	}
}

func TestSQLiteStore_UpsertChunkTask_TrimsRelPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := st.UpsertChunkTask(ctx, model.NewChunkTask(1, "trim me", "text", model.ChunkMetadata{
		ChunkID: 1,
		RelPath: "  docs/a.md  ",
		DocType: "md",
		RepType: "raw_text",
	})); err != nil {
		t.Fatalf("UpsertChunkTask failed: %v", err)
	}

	tasks, err := st.NextPending(ctx, 10, "text")
	if err != nil {
		t.Fatalf("NextPending failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Metadata.RelPath != "docs/a.md" {
		t.Fatalf("expected trimmed rel_path, got %q", tasks[0].Metadata.RelPath)
	}
}

func TestSQLiteStore_ClearDocumentContentHashes(t *testing.T) {
	st := NewSQLiteStore(filepath.Join(t.TempDir(), "meta.sqlite"))
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	doc := model.Document{
		RelPath:     "docs/a.md",
		DocType:     "md",
		SizeBytes:   12,
		MTimeUnix:   123,
		ContentHash: "abc123",
		Status:      "ready",
	}
	if err := st.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}
	got, err := st.GetDocumentByPath(ctx, "docs/a.md")
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}
	if got.ContentHash == "" {
		t.Fatalf("expected non-empty content_hash before clear")
	}

	if err := st.ClearDocumentContentHashes(ctx); err != nil {
		t.Fatalf("ClearDocumentContentHashes failed: %v", err)
	}
	got, err = st.GetDocumentByPath(ctx, "docs/a.md")
	if err != nil {
		t.Fatalf("GetDocumentByPath failed after clear: %v", err)
	}
	if got.ContentHash != "" {
		t.Fatalf("expected content_hash cleared, got %q", got.ContentHash)
	}
}

func TestSQLiteStore_EnsureDB_ConcurrentInitClose(t *testing.T) {
	// this test exercises the window addressed by the recent race fix: calling
	// ensureDB and Close simultaneously should not trigger a nil-pointer or
	// other panics.  with the mutex held during initialization there is no
	// race, but run under -race to be sure.
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	// don't defer Close here; we call it explicitly below

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// ignore result; we only care that it doesn't panic or cause race
		if db, err := st.ensureDB(ctx); err == nil {
			st.ReleaseDB()
			_ = db
		}
	}()
	go func() {
		defer wg.Done()
		_ = st.Close()
	}()
	wg.Wait()

	// store should be safely closed now; further Close is no-op
	if err := st.Close(); err != nil {
		t.Fatalf("unexpected error on second Close: %v", err)
	}
}

// verifyChunkIndexes ensures the sqlite initialization created the indexes we
// added to avoid full table scans. A missing index would mean queries like
// NextPending or path lookups could be slow.
func verifyChunkIndexes(ctx context.Context, st *SQLiteStore) error {
	db, err := st.ensureDB(ctx)
	if err != nil {
		return err
	}
	defer st.ReleaseDB()
	rows, err := db.QueryContext(ctx, "PRAGMA index_list('chunks')")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	present := make(map[string]bool)
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, want := range []string{
		"idx_chunks_embedding_status",
		"idx_chunks_index_kind",
		"idx_chunks_rel_path_deleted",
	} {
		if !present[want] {
			return fmt.Errorf("expected index %s to exist", want)
		}
	}
	return nil
}
func TestSQLiteStore_WithTx_Rollback(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "meta.sqlite")
	st := NewSQLiteStore(dbPath)
	defer func() { _ = st.Close() }()

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// perform an operation that will be rolled back
	err := st.WithTx(ctx, func(tx model.RepresentationStore) error {
		_, err := tx.UpsertRepresentation(ctx, model.Representation{DocID: 1, RepType: "raw_text", RepHash: "abc"})
		if err != nil {
			return err
		}
		return fmt.Errorf("force rollback")
	})
	if err == nil {
		t.Fatal("expected error from transactional callback")
	}

	// verify the representation was not persisted
	db, err := st.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB: %v", err)
	}
	// release when we're done to ensure activeOps drops to zero
	defer st.ReleaseDB()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM representations").Scan(&count); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 representations after rollback, got %d", count)
	}
}
