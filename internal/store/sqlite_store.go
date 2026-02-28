package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type SQLiteStore struct {
	path string

	mu          sync.RWMutex
	sqlite3Path string
	initialized bool
}

func NewSQLiteStore(path string) *SQLiteStore {
	return &SQLiteStore{path: path}
}

func (s *SQLiteStore) Init(ctx context.Context) error {
	if s == nil {
		return errors.New("nil store")
	}

	dbPath := strings.TrimSpace(s.path)
	if dbPath == "" {
		return errors.New("sqlite path is required")
	}

	stateDir := filepath.Dir(dbPath)
	if stateDir != "." {
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
	}

	sqlite3Path, err := exec.LookPath("sqlite3")
	if err != nil {
		return fmt.Errorf("sqlite3 binary not found in PATH: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sqlite3Path = sqlite3Path
	s.initialized = true

	if err := s.execScriptLocked(ctx, initScript()); err != nil {
		s.initialized = false
		return err
	}

	if err := s.bootstrapSettingsLocked(ctx); err != nil {
		s.initialized = false
		return err
	}

	return nil
}

func (s *SQLiteStore) UpsertDocument(ctx context.Context, doc model.Document) error {
	relPath, err := normalizeRelPath(doc.RelPath)
	if err != nil {
		return err
	}

	status := normalizeStatus(doc.Status)
	docType := normalizeDocType(doc.DocType)
	sourceType := "file"
	contentHash := strings.TrimSpace(doc.ContentHash)

	script := fmt.Sprintf(
		`INSERT INTO documents (
			rel_path, source_type, doc_type, size_bytes, mtime_unix, content_hash, status, error, deleted
		) VALUES (%s, %s, %s, %d, %d, %s, %s, NULL, %d)
		ON CONFLICT(rel_path) DO UPDATE SET
			source_type = excluded.source_type,
			doc_type = excluded.doc_type,
			size_bytes = excluded.size_bytes,
			mtime_unix = excluded.mtime_unix,
			content_hash = excluded.content_hash,
			status = excluded.status,
			error = excluded.error,
			deleted = excluded.deleted;`,
		sqlQuote(relPath),
		sqlQuote(sourceType),
		sqlQuote(docType),
		doc.SizeBytes,
		doc.MTimeUnix,
		sqlQuote(contentHash),
		sqlQuote(status),
		boolToInt(doc.Deleted),
	)

	return s.execScript(ctx, script)
}

func (s *SQLiteStore) GetDocumentByPath(ctx context.Context, relPath string) (model.Document, error) {
	normalizedPath, err := normalizeRelPath(relPath)
	if err != nil {
		return model.Document{}, err
	}

	script := fmt.Sprintf(
		`SELECT doc_id, rel_path, doc_type, size_bytes, mtime_unix, content_hash, status, deleted
		FROM documents
		WHERE rel_path = %s
		LIMIT 1;`,
		sqlQuote(normalizedPath),
	)

	rows, err := s.queryRows(ctx, script)
	if err != nil {
		return model.Document{}, err
	}
	if len(rows) == 0 {
		return model.Document{}, os.ErrNotExist
	}

	row := rows[0]
	docID, err := rowInt64(row, "doc_id")
	if err != nil {
		return model.Document{}, err
	}
	sizeBytes, err := rowInt64(row, "size_bytes")
	if err != nil {
		return model.Document{}, err
	}
	mtimeUnix, err := rowInt64(row, "mtime_unix")
	if err != nil {
		return model.Document{}, err
	}
	deletedInt, err := rowInt64(row, "deleted")
	if err != nil {
		return model.Document{}, err
	}

	return model.Document{
		DocID:       docID,
		RelPath:     rowString(row, "rel_path"),
		DocType:     rowString(row, "doc_type"),
		SizeBytes:   sizeBytes,
		MTimeUnix:   mtimeUnix,
		ContentHash: rowString(row, "content_hash"),
		Status:      rowString(row, "status"),
		Deleted:     deletedInt != 0,
	}, nil
}

func (s *SQLiteStore) ListFiles(ctx context.Context, prefix, glob string, limit, offset int) ([]model.Document, int64, error) {
	if limit <= 0 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	whereSQL := buildListWhereClause(prefix, glob)

	countScript := "SELECT COUNT(*) AS total FROM documents" + whereSQL + ";"
	countRows, err := s.queryRows(ctx, countScript)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(countRows) > 0 {
		total, err = rowInt64(countRows[0], "total")
		if err != nil {
			return nil, 0, err
		}
	}

	listScript := fmt.Sprintf(
		`SELECT doc_id, rel_path, doc_type, size_bytes, mtime_unix, content_hash, status, deleted
		FROM documents%s
		ORDER BY rel_path ASC
		LIMIT %d OFFSET %d;`,
		whereSQL,
		limit,
		offset,
	)
	rows, err := s.queryRows(ctx, listScript)
	if err != nil {
		return nil, 0, err
	}

	docs := make([]model.Document, 0, len(rows))
	for _, row := range rows {
		docID, docErr := rowInt64(row, "doc_id")
		if docErr != nil {
			return nil, 0, docErr
		}
		sizeBytes, docErr := rowInt64(row, "size_bytes")
		if docErr != nil {
			return nil, 0, docErr
		}
		mtimeUnix, docErr := rowInt64(row, "mtime_unix")
		if docErr != nil {
			return nil, 0, docErr
		}
		deletedInt, docErr := rowInt64(row, "deleted")
		if docErr != nil {
			return nil, 0, docErr
		}

		docs = append(docs, model.Document{
			DocID:       docID,
			RelPath:     rowString(row, "rel_path"),
			DocType:     rowString(row, "doc_type"),
			SizeBytes:   sizeBytes,
			MTimeUnix:   mtimeUnix,
			ContentHash: rowString(row, "content_hash"),
			Status:      rowString(row, "status"),
			Deleted:     deletedInt != 0,
		})
	}

	return docs, total, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = false
	return nil
}

func (s *SQLiteStore) UpsertRepresentation(ctx context.Context, rep model.Representation) (int64, error) {
	if rep.DocID <= 0 {
		return 0, errors.New("doc_id must be > 0")
	}

	repType := strings.TrimSpace(rep.RepType)
	if repType == "" {
		repType = "raw_text"
	}
	repHash := strings.TrimSpace(rep.RepHash)
	if repHash == "" {
		return 0, errors.New("rep_hash must be non-empty")
	}
	createdUnix := rep.CreatedUnix
	if createdUnix <= 0 {
		createdUnix = time.Now().Unix()
	}

	script := fmt.Sprintf(
		`INSERT INTO representations (doc_id, rep_type, rep_hash, created_unix, meta_json, deleted)
		VALUES (%d, %s, %s, %d, '{}', %d)
		ON CONFLICT(doc_id, rep_type) DO UPDATE SET
			rep_hash = excluded.rep_hash,
			created_unix = excluded.created_unix,
			meta_json = excluded.meta_json,
			deleted = excluded.deleted;`,
		rep.DocID,
		sqlQuote(repType),
		sqlQuote(repHash),
		createdUnix,
		boolToInt(rep.Deleted),
	)
	if err := s.execScript(ctx, script); err != nil {
		return 0, err
	}

	query := fmt.Sprintf(
		`SELECT rep_id FROM representations WHERE doc_id = %d AND rep_type = %s LIMIT 1;`,
		rep.DocID,
		sqlQuote(repType),
	)
	rows, err := s.queryRows(ctx, query)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, errors.New("representation upsert did not return a row")
	}
	return rowInt64(rows[0], "rep_id")
}

func (s *SQLiteStore) InsertChunkWithSpans(ctx context.Context, chunk model.Chunk, spans []model.Span) (int64, error) {
	if chunk.RepID <= 0 {
		return 0, errors.New("rep_id must be > 0")
	}
	if strings.TrimSpace(chunk.Text) == "" {
		return 0, errors.New("chunk text must be non-empty")
	}

	indexKind := normalizeIndexKind(chunk.IndexKind)
	embeddingStatus := normalizeEmbeddingStatus(chunk.EmbeddingStatus)

	upsertChunk := fmt.Sprintf(
		`INSERT INTO chunks (rep_id, ordinal, text, text_hash, tokens_est, index_kind, embedding_status, embedding_error, deleted)
		VALUES (%d, %d, %s, %s, %d, %s, %s, %s, %d)
		ON CONFLICT(rep_id, ordinal) DO UPDATE SET
			text = excluded.text,
			text_hash = excluded.text_hash,
			tokens_est = excluded.tokens_est,
			index_kind = excluded.index_kind,
			embedding_status = excluded.embedding_status,
			embedding_error = excluded.embedding_error,
			deleted = excluded.deleted;`,
		chunk.RepID,
		chunk.Ordinal,
		sqlQuote(chunk.Text),
		sqlQuote(strings.TrimSpace(chunk.TextHash)),
		0,
		sqlQuote(indexKind),
		sqlQuote(embeddingStatus),
		sqlMaybeNull(strings.TrimSpace(chunk.EmbeddingError)),
		boolToInt(chunk.Deleted),
	)
	if err := s.execScript(ctx, upsertChunk); err != nil {
		return 0, err
	}

	chunkIDQuery := fmt.Sprintf(
		`SELECT chunk_id FROM chunks WHERE rep_id = %d AND ordinal = %d LIMIT 1;`,
		chunk.RepID,
		chunk.Ordinal,
	)
	chunkRows, err := s.queryRows(ctx, chunkIDQuery)
	if err != nil {
		return 0, err
	}
	if len(chunkRows) == 0 {
		return 0, errors.New("chunk upsert did not return a row")
	}
	chunkID, err := rowInt64(chunkRows[0], "chunk_id")
	if err != nil {
		return 0, err
	}

	spanStatements := []string{fmt.Sprintf("DELETE FROM spans WHERE chunk_id = %d;", chunkID)}
	for _, span := range spans {
		spanKind, startValue, endValue, spanErr := spanToRow(span)
		if spanErr != nil {
			return 0, spanErr
		}
		spanStatements = append(
			spanStatements,
			fmt.Sprintf(
				`INSERT INTO spans (chunk_id, span_kind, start, end, extra_json) VALUES (%d, %s, %d, %d, NULL);`,
				chunkID,
				sqlQuote(spanKind),
				startValue,
				endValue,
			),
		)
	}
	if err := s.execTransaction(ctx, spanStatements); err != nil {
		return 0, err
	}

	return chunkID, nil
}

func (s *SQLiteStore) GetChunksByRepID(ctx context.Context, repID int64) ([]model.Chunk, error) {
	if repID <= 0 {
		return nil, errors.New("rep_id must be > 0")
	}

	script := fmt.Sprintf(
		`SELECT chunk_id, rep_id, ordinal, text, text_hash, index_kind, embedding_status, embedding_error, deleted
		FROM chunks
		WHERE rep_id = %d
		ORDER BY ordinal ASC;`,
		repID,
	)

	rows, err := s.queryRows(ctx, script)
	if err != nil {
		return nil, err
	}

	out := make([]model.Chunk, 0, len(rows))
	for _, row := range rows {
		chunkID, scanErr := rowInt64(row, "chunk_id")
		if scanErr != nil {
			return nil, scanErr
		}
		parsedRepID, scanErr := rowInt64(row, "rep_id")
		if scanErr != nil {
			return nil, scanErr
		}
		ordinal, scanErr := rowInt64(row, "ordinal")
		if scanErr != nil {
			return nil, scanErr
		}
		deletedInt, scanErr := rowInt64(row, "deleted")
		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, model.Chunk{
			ChunkID:         chunkID,
			RepID:           parsedRepID,
			Ordinal:         int(ordinal),
			Text:            rowString(row, "text"),
			TextHash:        rowString(row, "text_hash"),
			IndexKind:       rowString(row, "index_kind"),
			EmbeddingStatus: rowString(row, "embedding_status"),
			EmbeddingError:  rowString(row, "embedding_error"),
			Deleted:         deletedInt != 0,
		})
	}
	return out, nil
}

func (s *SQLiteStore) MarkDocumentDeleted(ctx context.Context, relPath string) error {
	normalizedPath, err := normalizeRelPath(relPath)
	if err != nil {
		return err
	}

	statements := []string{
		fmt.Sprintf(`UPDATE documents SET deleted = 1 WHERE rel_path = %s;`, sqlQuote(normalizedPath)),
		fmt.Sprintf(
			`UPDATE representations SET deleted = 1
			WHERE doc_id IN (SELECT doc_id FROM documents WHERE rel_path = %s);`,
			sqlQuote(normalizedPath),
		),
		fmt.Sprintf(
			`UPDATE chunks SET deleted = 1
			WHERE rep_id IN (
				SELECT rep_id FROM representations
				WHERE doc_id IN (SELECT doc_id FROM documents WHERE rel_path = %s)
			);`,
			sqlQuote(normalizedPath),
		),
	}

	return s.execTransaction(ctx, statements)
}

func (s *SQLiteStore) SetSetting(ctx context.Context, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("setting key is required")
	}
	script := fmt.Sprintf(
		`INSERT INTO settings (key, value) VALUES (%s, %s)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value;`,
		sqlQuote(key),
		sqlQuote(value),
	)
	return s.execScript(ctx, script)
}

func (s *SQLiteStore) GetSetting(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("setting key is required")
	}
	script := fmt.Sprintf(`SELECT value FROM settings WHERE key = %s LIMIT 1;`, sqlQuote(key))
	rows, err := s.queryRows(ctx, script)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", os.ErrNotExist
	}
	return rowString(rows[0], "value"), nil
}

func (s *SQLiteStore) execScript(ctx context.Context, script string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.execScriptLocked(ctx, script)
}

func (s *SQLiteStore) execScriptLocked(ctx context.Context, script string) error {
	dbPath, sqlite3Path, err := s.ensureReadyLocked()
	if err != nil {
		return err
	}

	fullScript := pragmaPrefix() + script
	cmd := exec.CommandContext(ctx, sqlite3Path, "-cmd", ".timeout 5000", dbPath, fullScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sqlite exec failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *SQLiteStore) execTransaction(ctx context.Context, statements []string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.execTransactionLocked(ctx, statements)
}

func (s *SQLiteStore) execTransactionLocked(ctx context.Context, statements []string) error {
	if len(statements) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("BEGIN IMMEDIATE;")
	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		b.WriteString(trimmed)
		if !strings.HasSuffix(trimmed, ";") {
			b.WriteString(";")
		}
	}
	b.WriteString("COMMIT;")
	return s.execScriptLocked(ctx, b.String())
}

func (s *SQLiteStore) queryRows(ctx context.Context, script string) ([]map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dbPath, sqlite3Path, err := s.ensureReadyLocked()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, sqlite3Path, "-cmd", ".timeout 5000", "-json", dbPath, script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite query failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return []map[string]interface{}{}, nil
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, fmt.Errorf("decode sqlite json result: %w", err)
	}
	return rows, nil
}

func (s *SQLiteStore) ensureReadyLocked() (dbPath string, sqlite3Path string, err error) {
	if s == nil {
		return "", "", errors.New("nil store")
	}
	if !s.initialized {
		return "", "", errors.New("store is not initialized")
	}
	if strings.TrimSpace(s.sqlite3Path) == "" {
		return "", "", errors.New("sqlite3 binary is not configured")
	}
	if strings.TrimSpace(s.path) == "" {
		return "", "", errors.New("sqlite path is empty")
	}
	return s.path, s.sqlite3Path, nil
}

func initScript() string {
	return `
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		CREATE TABLE IF NOT EXISTS documents (
			doc_id INTEGER PRIMARY KEY AUTOINCREMENT,
			rel_path TEXT NOT NULL UNIQUE,
			source_type TEXT NOT NULL DEFAULT 'file',
			doc_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			mtime_unix INTEGER NOT NULL DEFAULT 0,
			content_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'ok',
			error TEXT,
			deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1))
		);
		CREATE TABLE IF NOT EXISTS representations (
			rep_id INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id INTEGER NOT NULL,
			rep_type TEXT NOT NULL,
			rep_hash TEXT NOT NULL,
			created_unix INTEGER NOT NULL,
			meta_json TEXT NOT NULL DEFAULT '{}',
			deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1)),
			UNIQUE(doc_id, rep_type),
			FOREIGN KEY (doc_id) REFERENCES documents(doc_id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS chunks (
			chunk_id INTEGER PRIMARY KEY AUTOINCREMENT,
			rep_id INTEGER NOT NULL,
			ordinal INTEGER NOT NULL,
			text TEXT NOT NULL,
			text_hash TEXT NOT NULL,
			tokens_est INTEGER NOT NULL DEFAULT 0,
			index_kind TEXT NOT NULL,
			embedding_status TEXT NOT NULL DEFAULT 'pending',
			embedding_error TEXT,
			deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1)),
			UNIQUE(rep_id, ordinal),
			FOREIGN KEY (rep_id) REFERENCES representations(rep_id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS spans (
			span_id INTEGER PRIMARY KEY AUTOINCREMENT,
			chunk_id INTEGER NOT NULL,
			span_kind TEXT NOT NULL,
			start INTEGER NOT NULL,
			end INTEGER NOT NULL,
			extra_json TEXT,
			FOREIGN KEY (chunk_id) REFERENCES chunks(chunk_id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_documents_rel_path ON documents(rel_path);
		CREATE INDEX IF NOT EXISTS idx_documents_deleted ON documents(deleted);
		CREATE INDEX IF NOT EXISTS idx_representations_doc_id ON representations(doc_id);
		CREATE INDEX IF NOT EXISTS idx_chunks_rep_id ON chunks(rep_id);
		CREATE INDEX IF NOT EXISTS idx_chunks_embedding_status ON chunks(embedding_status);
		CREATE INDEX IF NOT EXISTS idx_spans_chunk_id ON spans(chunk_id);
	`
}

func (s *SQLiteStore) bootstrapSettingsLocked(ctx context.Context) error {
	defaults := map[string]string{
		"schema_version":       "1",
		"protocol_version":     "2025-11-25",
		"index_format_version": "1",
		"embed_text_model":     "mistral-embed",
		"embed_code_model":     "codestral-embed",
		"ocr_model":            "mistral-ocr-latest",
		"stt_provider":         "mistral",
		"stt_model":            "voxtral-mini-latest",
		"chat_model":           "mistral-small-2506",
	}

	statements := make([]string, 0, len(defaults))
	for key, value := range defaults {
		statements = append(
			statements,
			fmt.Sprintf(
				`INSERT INTO settings (key, value) VALUES (%s, %s)
				ON CONFLICT(key) DO NOTHING;`,
				sqlQuote(key),
				sqlQuote(value),
			),
		)
	}

	return s.execTransactionLocked(ctx, statements)
}

func buildListWhereClause(prefix, glob string) string {
	filters := make([]string, 0, 2)
	if normalizedPrefix := normalizePrefix(prefix); normalizedPrefix != "" {
		filters = append(filters, "rel_path LIKE "+sqlQuote(normalizedPrefix+"%"))
	}
	if trimmedGlob := strings.TrimSpace(glob); trimmedGlob != "" {
		filters = append(filters, "rel_path GLOB "+sqlQuote(trimmedGlob))
	}
	if len(filters) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(filters, " AND ")
}

func normalizeRelPath(relPath string) (string, error) {
	normalized := strings.TrimSpace(relPath)
	normalized = strings.TrimPrefix(normalized, "./")
	normalized = filepath.ToSlash(filepath.Clean(normalized))
	normalized = strings.TrimPrefix(normalized, "/")
	if normalized == "" || normalized == "." {
		return "", errors.New("rel_path must be non-empty")
	}
	return normalized, nil
}

func normalizePrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = filepath.ToSlash(filepath.Clean(trimmed))
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "." {
		return ""
	}
	return trimmed
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "skipped":
		return "skipped"
	case "error":
		return "error"
	default:
		return "ok"
	}
}

func normalizeDocType(docType string) string {
	docType = strings.TrimSpace(docType)
	if docType == "" {
		return "text"
	}
	return docType
}

func normalizeIndexKind(indexKind string) string {
	switch strings.ToLower(strings.TrimSpace(indexKind)) {
	case "code":
		return "code"
	default:
		return "text"
	}
}

func normalizeEmbeddingStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok":
		return "ok"
	case "error":
		return "error"
	default:
		return "pending"
	}
}

func spanToRow(span model.Span) (kind string, start int, end int, err error) {
	switch strings.ToLower(strings.TrimSpace(span.Kind)) {
	case "lines":
		if span.StartLine <= 0 || span.EndLine <= 0 || span.EndLine < span.StartLine {
			return "", 0, 0, errors.New("invalid lines span")
		}
		return "lines", span.StartLine, span.EndLine, nil
	case "page":
		if span.Page <= 0 {
			return "", 0, 0, errors.New("invalid page span")
		}
		return "page", span.Page, span.Page, nil
	case "time":
		if span.StartMS < 0 || span.EndMS < 0 || span.EndMS < span.StartMS {
			return "", 0, 0, errors.New("invalid time span")
		}
		return "time", span.StartMS, span.EndMS, nil
	default:
		return "", 0, 0, fmt.Errorf("unsupported span kind: %q", span.Kind)
	}
}

func rowString(row map[string]interface{}, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func rowInt64(row map[string]interface{}, key string) (int64, error) {
	value, ok := row[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("missing numeric column %q", key)
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed), nil
	case float32:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse numeric column %q: %w", key, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported numeric type for column %q: %T", key, value)
	}
}

func sqlQuote(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

func sqlMaybeNull(value string) string {
	if strings.TrimSpace(value) == "" {
		return "NULL"
	}
	return sqlQuote(value)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func pragmaPrefix() string {
	return "PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"
}
