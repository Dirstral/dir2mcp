package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type SQLiteStore struct {
	path string

	mu sync.Mutex
	db *sql.DB
}

func NewSQLiteStore(path string) *SQLiteStore {
	return &SQLiteStore{path: path}
}

func (s *SQLiteStore) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return nil
	}

	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return err
	}

	schema := `
CREATE TABLE IF NOT EXISTS documents (
  doc_id INTEGER PRIMARY KEY AUTOINCREMENT,
  rel_path TEXT NOT NULL UNIQUE,
  doc_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  mtime_unix INTEGER NOT NULL DEFAULT 0,
  content_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'ok',
  deleted INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS chunks (
  chunk_id INTEGER PRIMARY KEY,
  rel_path TEXT NOT NULL,
  doc_type TEXT NOT NULL,
  rep_type TEXT NOT NULL DEFAULT 'raw_text',
  text TEXT NOT NULL,
  index_kind TEXT NOT NULL DEFAULT 'text',
  embedding_status TEXT NOT NULL DEFAULT 'pending',
  embedding_error TEXT NOT NULL DEFAULT '',
  deleted INTEGER NOT NULL DEFAULT 0
);

-- indexes to speed up common lookups; part of the same initialization so they
-- are available immediately for NextPending and any rel_path queries.
CREATE INDEX IF NOT EXISTS idx_chunks_embedding_status ON chunks(embedding_status);
CREATE INDEX IF NOT EXISTS idx_chunks_index_kind ON chunks(index_kind);
-- rel_path lookups often filter deleted rows; use a composite index to cover
-- both columns and avoid scanning the entire table.
CREATE INDEX IF NOT EXISTS idx_chunks_rel_path_deleted ON chunks(rel_path, deleted);
`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	return nil
}

func (s *SQLiteStore) UpsertDocument(ctx context.Context, doc model.Document) error {
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(
		ctx,
		`INSERT INTO documents(rel_path, doc_type, size_bytes, mtime_unix, content_hash, status, deleted)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(rel_path) DO UPDATE SET
		   doc_type=excluded.doc_type,
		   size_bytes=excluded.size_bytes,
		   mtime_unix=excluded.mtime_unix,
		   content_hash=excluded.content_hash,
		   status=excluded.status,
		   deleted=excluded.deleted`,
		doc.RelPath,
		defaultIfEmpty(doc.DocType, "text"),
		doc.SizeBytes,
		doc.MTimeUnix,
		doc.ContentHash,
		defaultIfEmpty(doc.Status, "ok"),
		boolToInt(doc.Deleted),
	)
	return err
}

func (s *SQLiteStore) UpsertChunkTask(ctx context.Context, task model.ChunkTask) error {
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	if task.Label == 0 {
		return errors.New("task label is required")
	}

	_, err = db.ExecContext(
		ctx,
		`INSERT INTO chunks(chunk_id, rel_path, doc_type, rep_type, text, index_kind, embedding_status, embedding_error, deleted)
		 VALUES(?, ?, ?, ?, ?, ?, 'pending', '', 0)
		 ON CONFLICT(chunk_id) DO UPDATE SET
		   rel_path=excluded.rel_path,
		   doc_type=excluded.doc_type,
		   rep_type=excluded.rep_type,
		   text=excluded.text,
		   index_kind=excluded.index_kind,
		   deleted=0,
		   embedding_status='pending',
		   embedding_error=''`,
		int64(task.Label),
		defaultIfEmpty(task.Metadata.RelPath, ""),
		defaultIfEmpty(task.Metadata.DocType, "unknown"),
		defaultIfEmpty(task.Metadata.RepType, "raw_text"),
		task.Text,
		defaultIfEmpty(task.IndexKind, "text"),
	)
	return err
}

func (s *SQLiteStore) GetDocumentByPath(ctx context.Context, relPath string) (model.Document, error) {
	db, err := s.ensureDB(ctx)
	if err != nil {
		return model.Document{}, err
	}

	var doc model.Document
	var deleted int
	row := db.QueryRowContext(
		ctx,
		`SELECT doc_id, rel_path, doc_type, size_bytes, mtime_unix, content_hash, status, deleted
		 FROM documents WHERE rel_path = ?`,
		relPath,
	)
	if err := row.Scan(
		&doc.DocID,
		&doc.RelPath,
		&doc.DocType,
		&doc.SizeBytes,
		&doc.MTimeUnix,
		&doc.ContentHash,
		&doc.Status,
		&deleted,
	); err != nil {
		return model.Document{}, err
	}
	doc.Deleted = deleted == 1
	return doc, nil
}

func (s *SQLiteStore) ListFiles(ctx context.Context, prefix, glob string, limit, offset int) ([]model.Document, int64, error) {
	db, err := s.ensureDB(ctx)
	if err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT doc_id, rel_path, doc_type, size_bytes, mtime_unix, content_hash, status, deleted FROM documents`
	where := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if strings.TrimSpace(prefix) != "" {
		where = append(where, "rel_path LIKE ?")
		args = append(args, prefix+"%")
	}
	if strings.TrimSpace(glob) != "" {
		where = append(where, "rel_path GLOB ?")
		args = append(args, glob)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY rel_path LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	docs := make([]model.Document, 0, limit)
	for rows.Next() {
		var doc model.Document
		var deleted int
		if err := rows.Scan(
			&doc.DocID,
			&doc.RelPath,
			&doc.DocType,
			&doc.SizeBytes,
			&doc.MTimeUnix,
			&doc.ContentHash,
			&doc.Status,
			&deleted,
		); err != nil {
			return nil, 0, err
		}
		doc.Deleted = deleted == 1
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM documents"
	countArgs := make([]any, 0)
	if len(where) > 0 {
		countQuery += " WHERE " + strings.Join(where, " AND ")
		countArgs = append(countArgs, args[:len(args)-2]...)
	}
	if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	return docs, total, nil
}

func (s *SQLiteStore) NextPending(ctx context.Context, limit int, indexKind string) ([]model.ChunkTask, error) {
	db, err := s.ensureDB(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 32
	}

	// build argument list incrementally: always start with the pending status,
	// optionally filter by indexKind, and append the limit last so positional
	// placeholders in the query (`?`) remain in order.
	args := []any{"pending"}
	query := `SELECT chunk_id, rel_path, doc_type, rep_type, text, index_kind FROM chunks WHERE embedding_status = ? AND deleted = 0`
	if strings.TrimSpace(indexKind) != "" {
		query += " AND index_kind = ?"
		args = append(args, indexKind)
	}
	// limit placeholder comes after any indexKind parameter
	args = append(args, limit)
	query += " ORDER BY chunk_id LIMIT ?"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]model.ChunkTask, 0, limit)
	for rows.Next() {
		var (
			chunkID int64
			relPath string
			docType string
			repType string
			text    string
			idxKind string
		)
		if err := rows.Scan(&chunkID, &relPath, &docType, &repType, &text, &idxKind); err != nil {
			return nil, err
		}
		tasks = append(tasks, model.ChunkTask{
			Label:     chunkID,
			Text:      text,
			IndexKind: idxKind,
			Metadata: model.ChunkMetadata{
				ChunkID: chunkID,
				RelPath: relPath,
				DocType: docType,
				RepType: repType,
				Snippet: snippet(text, 240),
				Span:    model.Span{Kind: "lines"},
			},
		})
	}
	return tasks, rows.Err()
}

func (s *SQLiteStore) ListEmbeddedChunkMetadata(ctx context.Context, indexKind string, limit, offset int) ([]model.ChunkTask, error) {
	db, err := s.ensureDB(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	args := []any{"ok", limit, offset}
	query := `SELECT chunk_id, rel_path, doc_type, rep_type, text, index_kind
	          FROM chunks
	          WHERE embedding_status = ? AND deleted = 0`
	if strings.TrimSpace(indexKind) != "" {
		query += ` AND index_kind = ?`
		args = []any{"ok", indexKind, limit, offset}
	}
	query += ` ORDER BY chunk_id LIMIT ? OFFSET ?`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]model.ChunkTask, 0, limit)
	for rows.Next() {
		var (
			chunkID int64
			relPath string
			docType string
			repType string
			text    string
			kind    string
		)
		if err := rows.Scan(&chunkID, &relPath, &docType, &repType, &text, &kind); err != nil {
			return nil, err
		}
		out = append(out, model.ChunkTask{
			Label:     chunkID,
			Text:      text,
			IndexKind: kind,
			Metadata: model.ChunkMetadata{
				ChunkID: chunkID,
				RelPath: relPath,
				DocType: docType,
				RepType: repType,
				Snippet: snippet(text, 240),
				Span:    model.Span{Kind: "lines"},
			},
		})
	}
	return out, rows.Err()
}

func (s *SQLiteStore) MarkEmbedded(ctx context.Context, labels []int64) error {
	return s.markEmbeddingStatus(ctx, labels, "ok", "")
}

func (s *SQLiteStore) MarkFailed(ctx context.Context, labels []int64, reason string) error {
	return s.markEmbeddingStatus(ctx, labels, "error", reason)
}

func (s *SQLiteStore) markEmbeddingStatus(ctx context.Context, labels []int64, status, reason string) error {
	if len(labels) == 0 {
		return nil
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `UPDATE chunks SET embedding_status = ?, embedding_error = ? WHERE chunk_id = ?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, label := range labels {
		if _, err := stmt.ExecContext(ctx, status, reason, label); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *SQLiteStore) ensureDB(ctx context.Context) (*sql.DB, error) {
	if err := s.Init(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("sqlite db not initialized")
	}
	return s.db, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func snippet(text string, max int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
