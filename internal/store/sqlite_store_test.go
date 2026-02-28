package store

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/model"
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

	if err := st.UpsertChunkTask(ctx, model.ChunkTask{
		Label:     101,
		Text:      "chunk text",
		IndexKind: "text",
		Metadata: model.ChunkMetadata{
			ChunkID: 101,
			RelPath: "docs/a.md",
			DocType: "md",
			RepType: "raw_text",
		},
	}); err != nil {
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
	if tasks[0].Label != 101 ||
		tasks[0].Text != expectedText ||
		tasks[0].IndexKind != expectedIndexKind ||
		!reflect.DeepEqual(tasks[0].Metadata, expectedMetadata) {
		t.Fatalf("unexpected task fields: %#v", tasks)
	}

	if err := st.MarkEmbedded(ctx, []int64{101}); err != nil {
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

// verifyChunkIndexes ensures the sqlite initialization created the indexes we
// added to avoid full table scans. A missing index would mean queries like
// NextPending or path lookups could be slow.
func verifyChunkIndexes(ctx context.Context, st *SQLiteStore) error {
	db, err := st.ensureDB(ctx)
	if err != nil {
		return err
	}
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
