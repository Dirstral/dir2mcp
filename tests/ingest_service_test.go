package tests

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/config"
	"dir2mcp/internal/ingest"
	"dir2mcp/internal/model"
)

func TestServiceRun_ProcessesFilesAndMarksMissingDeleted(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), []byte("plain text"))
	mustWriteFile(t, filepath.Join(root, "code", "main.go"), []byte("package main\n"))
	mustWriteFile(t, filepath.Join(root, "archive.zip"), []byte("PK\x03\x04"))
	mustWriteFile(t, filepath.Join(root, "secret.txt"), []byte("Authorization: Bearer abcdefgh.ijklmnop.qrstuvwx\n"))
	mustWriteFile(t, filepath.Join(root, "exclude", "private.txt"), []byte("should be excluded"))

	st := newMemoryStore()
	st.docs["gone.txt"] = model.Document{
		RelPath:   "gone.txt",
		DocType:   "text",
		SizeBytes: 4,
		MTimeUnix: 1,
		Status:    "ok",
	}
	st.docs["exclude/private.txt"] = model.Document{
		RelPath:   "exclude/private.txt",
		DocType:   "text",
		SizeBytes: 1,
		MTimeUnix: 1,
		Status:    "ok",
	}

	cfg := config.Default()
	cfg.RootDir = root
	cfg.Security.PathExcludes = []string{"**/exclude/**"}

	indexState := appstate.NewIndexingState(appstate.ModeIncremental)
	svc := ingest.NewService(cfg, st)
	svc.SetIndexingState(indexState)

	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	snapshot := indexState.Snapshot()
	if snapshot.Scanned != 5 {
		t.Fatalf("snapshot.Scanned=%d want=5", snapshot.Scanned)
	}
	if snapshot.Indexed != 2 {
		t.Fatalf("snapshot.Indexed=%d want=2", snapshot.Indexed)
	}
	if snapshot.Skipped != 3 {
		t.Fatalf("snapshot.Skipped=%d want=3", snapshot.Skipped)
	}
	if snapshot.Deleted != 2 {
		t.Fatalf("snapshot.Deleted=%d want=2", snapshot.Deleted)
	}
	if snapshot.Errors != 0 {
		t.Fatalf("snapshot.Errors=%d want=0", snapshot.Errors)
	}

	keep := st.docs["keep.txt"]
	if keep.Status != "ok" {
		t.Fatalf("keep.txt status=%q want=ok", keep.Status)
	}
	if keep.DocType != "text" {
		t.Fatalf("keep.txt doc_type=%q want=text", keep.DocType)
	}
	if keep.ContentHash == "" {
		t.Fatal("keep.txt content hash should not be empty")
	}

	code := st.docs["code/main.go"]
	if code.Status != "ok" || code.DocType != "code" {
		t.Fatalf("code/main.go unexpected doc: %#v", code)
	}

	archive := st.docs["archive.zip"]
	if archive.Status != "skipped" || archive.DocType != "archive" {
		t.Fatalf("archive.zip unexpected doc: %#v", archive)
	}

	secret := st.docs["secret.txt"]
	if secret.Status != "secret_excluded" {
		t.Fatalf("secret.txt status=%q want=secret_excluded", secret.Status)
	}

	excluded := st.docs["exclude/private.txt"]
	if !excluded.Deleted {
		t.Fatalf("exclude/private.txt should be marked deleted, got %#v", excluded)
	}

	gone := st.docs["gone.txt"]
	if !gone.Deleted {
		t.Fatalf("gone.txt should be marked deleted, got %#v", gone)
	}
}

func TestServiceRun_ReturnsErrorOnInvalidSecretPattern(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), []byte("plain text"))

	cfg := config.Default()
	cfg.RootDir = root
	cfg.Security.SecretPatterns = []string{"["}

	svc := ingest.NewService(cfg, newMemoryStore())
	if err := svc.Run(context.Background()); err == nil {
		t.Fatal("expected error for invalid secret pattern")
	}
}

func TestServiceRun_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), []byte("plain text"))

	cfg := config.Default()
	cfg.RootDir = root

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := ingest.NewService(cfg, newMemoryStore())
	if err := svc.Run(ctx); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

type memoryStore struct {
	docs map[string]model.Document
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		docs: make(map[string]model.Document),
	}
}

func (s *memoryStore) Init(_ context.Context) error { return nil }

func (s *memoryStore) UpsertDocument(_ context.Context, doc model.Document) error {
	current, ok := s.docs[doc.RelPath]
	if ok {
		doc.DocID = current.DocID
	} else {
		doc.DocID = int64(len(s.docs) + 1)
	}
	s.docs[doc.RelPath] = doc
	return nil
}

func (s *memoryStore) GetDocumentByPath(_ context.Context, relPath string) (model.Document, error) {
	doc, ok := s.docs[relPath]
	if !ok {
		return model.Document{}, os.ErrNotExist
	}
	return doc, nil
}

func (s *memoryStore) ListFiles(_ context.Context, prefix, glob string, limit, offset int) ([]model.Document, int64, error) {
	if limit <= 0 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	keys := make([]string, 0, len(s.docs))
	for relPath := range s.docs {
		if strings.TrimSpace(prefix) != "" && !strings.HasPrefix(relPath, prefix) {
			continue
		}
		if strings.TrimSpace(glob) != "" {
			match, err := path.Match(glob, relPath)
			if err != nil || !match {
				continue
			}
		}
		keys = append(keys, relPath)
	}
	sort.Strings(keys)

	total := int64(len(keys))
	if offset >= len(keys) {
		return []model.Document{}, total, nil
	}
	end := offset + limit
	if end > len(keys) {
		end = len(keys)
	}

	out := make([]model.Document, 0, end-offset)
	for _, key := range keys[offset:end] {
		out = append(out, s.docs[key])
	}
	return out, total, nil
}

func (s *memoryStore) Close() error { return nil }

func (s *memoryStore) MarkDocumentDeleted(_ context.Context, relPath string) error {
	doc, ok := s.docs[relPath]
	if !ok {
		doc = model.Document{RelPath: relPath}
	}
	doc.Deleted = true
	s.docs[relPath] = doc
	return nil
}

func TestMemoryStoreListFilesPaging(t *testing.T) {
	st := newMemoryStore()
	st.docs["a.txt"] = model.Document{RelPath: "a.txt"}
	st.docs["b.txt"] = model.Document{RelPath: "b.txt"}
	st.docs["c.txt"] = model.Document{RelPath: "c.txt"}

	page1, total, err := st.ListFiles(context.Background(), "", "", 2, 0)
	if err != nil {
		t.Fatalf("ListFiles page1 failed: %v", err)
	}
	if total != 3 {
		t.Fatalf("total=%d want=3", total)
	}
	gotPage1 := []string{page1[0].RelPath, page1[1].RelPath}
	if !slices.Equal(gotPage1, []string{"a.txt", "b.txt"}) {
		t.Fatalf("page1 unexpected: %v", gotPage1)
	}

	page2, _, err := st.ListFiles(context.Background(), "", "", 2, 2)
	if err != nil {
		t.Fatalf("ListFiles page2 failed: %v", err)
	}
	if len(page2) != 1 || page2[0].RelPath != "c.txt" {
		t.Fatalf("page2 unexpected: %#v", page2)
	}
}
