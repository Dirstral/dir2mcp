package tests

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dir2mcp/internal/model"
	"dir2mcp/internal/store"
)

func TestSQLiteStoreInitBootstrapsSchemaAndSettings(t *testing.T) {
	ctx := context.Background()
	st := store.NewSQLiteStore(filepath.Join(t.TempDir(), "meta.sqlite"))
	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	keys := []string{
		"schema_version",
		"protocol_version",
		"index_format_version",
		"embed_text_model",
		"embed_code_model",
		"ocr_model",
		"stt_provider",
		"stt_model",
		"chat_model",
	}

	for _, key := range keys {
		value, err := st.GetSetting(ctx, key)
		if err != nil {
			t.Fatalf("GetSetting(%q) failed: %v", key, err)
		}
		if value == "" {
			t.Fatalf("GetSetting(%q) returned empty value", key)
		}
	}
}

func TestSQLiteStoreDocumentCRUDAndListFilters(t *testing.T) {
	ctx := context.Background()
	st := store.NewSQLiteStore(filepath.Join(t.TempDir(), "meta.sqlite"))
	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	docs := []model.Document{
		{
			RelPath:     "src/main.go",
			DocType:     "code",
			SizeBytes:   120,
			MTimeUnix:   1700000000,
			ContentHash: "hash-main",
			Status:      "ok",
		},
		{
			RelPath:     "src/lib/util.go",
			DocType:     "code",
			SizeBytes:   80,
			MTimeUnix:   1700000001,
			ContentHash: "hash-util",
			Status:      "ok",
		},
		{
			RelPath:     "docs/readme.md",
			DocType:     "md",
			SizeBytes:   64,
			MTimeUnix:   1700000002,
			ContentHash: "hash-readme",
			Status:      "skipped",
			Deleted:     true,
		},
	}
	for _, doc := range docs {
		if err := st.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument(%s) failed: %v", doc.RelPath, err)
		}
	}

	got, err := st.GetDocumentByPath(ctx, "./src/main.go")
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}
	if got.RelPath != "src/main.go" {
		t.Fatalf("unexpected RelPath: got=%q want=%q", got.RelPath, "src/main.go")
	}
	if got.DocType != "code" {
		t.Fatalf("unexpected DocType: got=%q want=%q", got.DocType, "code")
	}

	srcDocs, total, err := st.ListFiles(ctx, "src", "", 100, 0)
	if err != nil {
		t.Fatalf("ListFiles(prefix) failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("unexpected prefix total: got=%d want=2", total)
	}
	if len(srcDocs) != 2 {
		t.Fatalf("unexpected prefix result count: got=%d want=2", len(srcDocs))
	}

	// deleted documents must not appear in ListFiles results
	mdDocs, mdTotal, err := st.ListFiles(ctx, "", "docs/*.md", 100, 0)
	if err != nil {
		t.Fatalf("ListFiles(glob) failed: %v", err)
	}
	if mdTotal != 0 || len(mdDocs) != 0 {
		t.Fatalf("deleted doc must be excluded: total=%d len=%d", mdTotal, len(mdDocs))
	}

	// total across all docs excludes the deleted entry (3 upserted, 1 deleted = 2 live)
	pageOne, totalWithPaging, err := st.ListFiles(ctx, "", "", 1, 1)
	if err != nil {
		t.Fatalf("ListFiles(paging) failed: %v", err)
	}
	if totalWithPaging != 2 {
		t.Fatalf("unexpected total with paging: got=%d want=2", totalWithPaging)
	}
	if len(pageOne) != 1 {
		t.Fatalf("unexpected page size: got=%d want=1", len(pageOne))
	}
}

func TestSQLiteStoreRejectsAbsoluteAndTraversalRelPaths(t *testing.T) {
	ctx := context.Background()
	st := store.NewSQLiteStore(filepath.Join(t.TempDir(), "meta.sqlite"))
	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	absolutePath := filepath.Join(t.TempDir(), "escape.txt")
	invalidPaths := []string{
		absolutePath,
		"../escape.txt",
		"..",
		"a/../../b.txt",
	}

	for _, relPath := range invalidPaths {
		err := st.UpsertDocument(ctx, model.Document{
			RelPath:     relPath,
			DocType:     "text",
			SizeBytes:   12,
			MTimeUnix:   1700000999,
			ContentHash: "invalid",
			Status:      "ok",
		})
		if err == nil {
			t.Fatalf("expected invalid rel_path to fail: %q", relPath)
		}
	}

	if err := st.UpsertDocument(ctx, model.Document{
		RelPath:     "safe/path.txt",
		DocType:     "text",
		SizeBytes:   12,
		MTimeUnix:   1700001000,
		ContentHash: "valid",
		Status:      "ok",
	}); err != nil {
		t.Fatalf("expected valid rel_path to succeed: %v", err)
	}
}

func TestSQLiteStoreRepresentationChunkSpanAndDeleteCascade(t *testing.T) {
	ctx := context.Background()
	st := store.NewSQLiteStore(filepath.Join(t.TempDir(), "meta.sqlite"))
	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	if err := st.UpsertDocument(ctx, model.Document{
		RelPath:     "notes/todo.txt",
		DocType:     "text",
		SizeBytes:   42,
		MTimeUnix:   1700000100,
		ContentHash: "doc-hash",
		Status:      "ok",
	}); err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}
	doc, err := st.GetDocumentByPath(ctx, "notes/todo.txt")
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}

	repID, err := st.UpsertRepresentation(ctx, model.Representation{
		DocID:       doc.DocID,
		RepType:     "raw_text",
		RepHash:     "rep-hash-v1",
		CreatedUnix: 1700000101,
	})
	if err != nil {
		t.Fatalf("UpsertRepresentation failed: %v", err)
	}
	if repID <= 0 {
		t.Fatalf("invalid repID: %d", repID)
	}

	chunkID, err := st.InsertChunkWithSpans(ctx, model.Chunk{
		RepID:           repID,
		Ordinal:         0,
		Text:            "hello world",
		TextHash:        "chunk-hash",
		IndexKind:       "text",
		EmbeddingStatus: "pending",
	}, []model.Span{
		{Kind: "lines", StartLine: 1, EndLine: 3},
	})
	if err != nil {
		t.Fatalf("InsertChunkWithSpans failed: %v", err)
	}
	if chunkID <= 0 {
		t.Fatalf("invalid chunkID: %d", chunkID)
	}

	chunks, err := st.GetChunksByRepID(ctx, repID)
	if err != nil {
		t.Fatalf("GetChunksByRepID failed: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("unexpected chunk count: got=%d want=1", len(chunks))
	}
	if chunks[0].Text != "hello world" {
		t.Fatalf("unexpected chunk text: %q", chunks[0].Text)
	}

	if err := st.MarkDocumentDeleted(ctx, doc.RelPath); err != nil {
		t.Fatalf("MarkDocumentDeleted failed: %v", err)
	}

	deletedDoc, err := st.GetDocumentByPath(ctx, doc.RelPath)
	if err != nil {
		t.Fatalf("GetDocumentByPath after delete failed: %v", err)
	}
	if !deletedDoc.Deleted {
		t.Fatal("expected document tombstone")
	}

	deletedChunks, err := st.GetChunksByRepID(ctx, repID)
	if err != nil {
		t.Fatalf("GetChunksByRepID after delete failed: %v", err)
	}
	if len(deletedChunks) != 1 || !deletedChunks[0].Deleted {
		t.Fatalf("expected chunk tombstone, got=%#v", deletedChunks)
	}
}

func TestSQLiteStoreConcurrentReadWriteWithWAL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := store.NewSQLiteStore(filepath.Join(t.TempDir(), "meta.sqlite"))
	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 250; i++ {
			err := st.UpsertDocument(ctx, model.Document{
				RelPath:     fmt.Sprintf("src/file_%03d.go", i),
				DocType:     "code",
				SizeBytes:   int64(200 + i),
				MTimeUnix:   int64(1700000200 + i),
				ContentHash: fmt.Sprintf("hash-%03d", i),
				Status:      "ok",
			})
			if err != nil {
				errCh <- fmt.Errorf("writer failed at %d: %w", i, err)
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 250; i++ {
			_, _, err := st.ListFiles(ctx, "src", "", 50, 0)
			if err != nil {
				errCh <- fmt.Errorf("reader failed at %d: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	got, total, err := st.ListFiles(ctx, "src", "", 300, 0)
	if err != nil {
		t.Fatalf("final ListFiles failed: %v", err)
	}
	if total != 250 || len(got) != 250 {
		t.Fatalf("unexpected final listing size total=%d len=%d", total, len(got))
	}
}
