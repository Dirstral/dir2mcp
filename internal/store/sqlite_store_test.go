package store

import (
	"context"
	"path/filepath"
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

	if err := st.UpsertChunkTask(ctx, model.ChunkTask{
		Label:     101,
		Text:      "chunk text",
		IndexKind: "text",
		Metadata: model.SearchHit{
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
	if len(tasks) != 1 || tasks[0].Label != 101 {
		t.Fatalf("unexpected tasks: %#v", tasks)
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

