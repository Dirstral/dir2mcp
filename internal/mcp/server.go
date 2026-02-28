package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/model"
)

const (
	sessionHeaderName      = "MCP-Session-Id"
	authTokenEnvVar        = "DIR2MCP_AUTH_TOKEN"
	maxRequestBody         = 1 << 20
	sessionTTL             = 24 * time.Hour
	sessionCleanupInterval = time.Hour
)

// DefaultSearchK is the default number of hits to request when a search
// query omits the `k` parameter or supplies a non‑positive value. This
// mirrors the JSON schema in SPEC.md which specifies a default of 10.
const DefaultSearchK = 10

type Server struct {
	cfg       config.Config
	authToken string
	retriever model.Retriever

	sessionMu sync.RWMutex
	sessions  map[string]time.Time
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    *rpcErrorData `json:"data,omitempty"`
}

type rpcErrorData struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

type validationError struct {
	message       string
	canonicalCode string
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type searchArgs struct {
	Query      string   `json:"query"`
	K          int      `json:"k"`
	Index      string   `json:"index"`
	PathPrefix string   `json:"path_prefix"`
	FileGlob   string   `json:"file_glob"`
	DocTypes   []string `json:"doc_types"`
}

type openFileArgs struct {
	RelPath   string `json:"rel_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Page      int    `json:"page"`
	StartMS   int    `json:"start_ms"`
	EndMS     int    `json:"end_ms"`
	MaxChars  int    `json:"max_chars"`
}

func (e validationError) Error() string {
	return e.message
}

func NewServer(cfg config.Config, retriever model.Retriever) *Server {
	return &Server{
		cfg:       cfg,
		authToken: loadAuthToken(cfg),
		retriever: retriever,
		sessions:  make(map[string]time.Time),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.MCPPath, s.handleMCP)
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	return s.RunOnListener(ctx, ln)
}

func (s *Server) RunOnListener(ctx context.Context, ln net.Listener) error {
	if ln == nil {
		return errors.New("nil listener passed to RunOnListener")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.runSessionCleanup(runCtx)

	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-runCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, nil, -32600, "Content-Type must be application/json", "INVALID_FIELD", false)
		return
	}

	if !s.authorize(w, r) {
		return
	}

	if !s.allowOrigin(w, r) {
		return
	}

	req, parseErr := parseRequest(r.Body)
	if parseErr != nil {
		canonicalCode := "INVALID_FIELD"
		var vErr validationError
		if errors.As(parseErr, &vErr) && vErr.canonicalCode != "" {
			canonicalCode = vErr.canonicalCode
		}
		writeError(w, http.StatusBadRequest, nil, -32600, parseErr.Error(), canonicalCode, false)
		return
	}

	id, hasID, idErr := parseID(req.ID)
	if idErr != nil {
		canonicalCode := "INVALID_FIELD"
		var vErr validationError
		if errors.As(idErr, &vErr) && vErr.canonicalCode != "" {
			canonicalCode = vErr.canonicalCode
		}
		writeError(w, http.StatusBadRequest, nil, -32600, idErr.Error(), canonicalCode, false)
		return
	}

	if req.Method == "" {
		writeError(w, http.StatusBadRequest, id, -32600, "method is required", "MISSING_FIELD", false)
		return
	}

	if req.Method != "initialize" {
		sessionID := strings.TrimSpace(r.Header.Get(sessionHeaderName))
		if sessionID == "" || !s.hasActiveSession(sessionID, time.Now()) {
			writeError(w, http.StatusNotFound, id, -32001, "session not found", "SESSION_NOT_FOUND", false)
			return
		}
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, id, hasID)
	case "notifications/initialized":
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeResult(w, http.StatusOK, id, map[string]interface{}{})
	case "tools/list":
		s.handleToolsList(w, id, hasID)
	case "tools/call":
		s.handleToolsCall(r.Context(), w, id, hasID, req.Params)
	default:
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeError(w, http.StatusOK, id, -32601, "method not found", "METHOD_NOT_FOUND", false)
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, id interface{}, hasID bool) {
	if !hasID {
		writeError(w, http.StatusBadRequest, nil, -32600, "initialize requires id", "MISSING_FIELD", false)
		return
	}

	sessionID, err := generateSessionID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, id, -32603, "failed to initialize session", "", false)
		return
	}
	s.storeSession(sessionID)

	w.Header().Set(sessionHeaderName, sessionID)
	writeResult(w, http.StatusOK, id, map[string]interface{}{
		"protocolVersion": s.cfg.ProtocolVersion,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]interface{}{
			"name":    "dir2mcp",
			"title":   "dir2mcp: Directory RAG MCP Server",
			"version": "0.0.0-dev",
		},
		"instructions": "Use tools/list then tools/call. Results include citations.",
	})
}

func (s *Server) handleToolsList(w http.ResponseWriter, id interface{}, hasID bool) {
	if !hasID {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	searchTool := map[string]interface{}{
		"name":        "dir2mcp.search",
		"description": "Semantic retrieval across indexed content.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":       map[string]interface{}{"type": "string"},
				"k":           map[string]interface{}{"type": "integer", "default": 10},
				"index":       map[string]interface{}{"type": "string", "enum": []string{"auto", "text", "code", "both"}},
				"path_prefix": map[string]interface{}{"type": "string"},
				"file_glob":   map[string]interface{}{"type": "string"},
				"doc_types": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"query"},
		},
	}
	openFileTool := map[string]interface{}{
		"name":        "dir2mcp.open_file",
		"description": "Open an exact source slice for verification (lines/page/time).",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"rel_path":   map[string]interface{}{"type": "string"},
				"start_line": map[string]interface{}{"type": "integer", "minimum": 1},
				"end_line":   map[string]interface{}{"type": "integer", "minimum": 1},
				"page":       map[string]interface{}{"type": "integer", "minimum": 1},
				"start_ms":   map[string]interface{}{"type": "integer", "minimum": 0},
				"end_ms":     map[string]interface{}{"type": "integer", "minimum": 0},
				"max_chars":  map[string]interface{}{"type": "integer", "minimum": 200, "maximum": 50000, "default": 20000},
			},
			"required": []string{"rel_path"},
		},
	}

	writeResult(w, http.StatusOK, id, map[string]interface{}{
		"tools": []interface{}{searchTool, openFileTool},
	})
}

func (s *Server) handleToolsCall(ctx context.Context, w http.ResponseWriter, id interface{}, hasID bool, raw json.RawMessage) {
	if !hasID {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if s.retriever == nil {
		writeError(w, http.StatusOK, id, -32000, "retriever not configured", "INDEX_NOT_READY", false)
		return
	}

	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, id, -32602, "params is required", "MISSING_FIELD", false)
		return
	}
	var params toolsCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		writeError(w, http.StatusBadRequest, id, -32602, "invalid params", "INVALID_FIELD", false)
		return
	}

	switch params.Name {
	case "dir2mcp.search":
		var args searchArgs
		if len(params.Arguments) > 0 {
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				writeError(w, http.StatusBadRequest, id, -32602, "invalid tool arguments", "INVALID_FIELD", false)
				return
			}
		}
		if strings.TrimSpace(args.Query) == "" {
			writeError(w, http.StatusBadRequest, id, -32602, "query is required", "MISSING_FIELD", false)
			return
		}

		// k is optional.  If the client does not provide a value (or provides a
		// non‑positive one) we fall back to a sensible default rather than return
		// an error.  This matches the `default` keyword in the JSON schema
		// defined in SPEC.md.
		if args.K <= 0 {
			args.K = DefaultSearchK
		}

		hits, err := s.retriever.Search(ctx, model.SearchQuery{
			Query:      args.Query,
			K:          args.K,
			Index:      args.Index,
			PathPrefix: args.PathPrefix,
			FileGlob:   args.FileGlob,
			DocTypes:   args.DocTypes,
		})
		if err != nil {
			log.Printf("tools/call search failed: id=%v tool=%s err=%v", id, params.Name, err)
			// choose a canonical code/message based on the error.  At present the
			// retriever may return an error when the index is not yet available or
			// configured; in that case we want the caller to see INDEX_NOT_READY
			// and a matching message.  Otherwise fall back to a generic internal
			// error code as defined in SPEC.md.
			canon := "INTERNAL_ERROR"
			msg := "internal server error"
			if errors.Is(err, model.ErrIndexNotReady) || errors.Is(err, model.ErrIndexNotConfigured) {
				canon = "INDEX_NOT_READY"
				msg = "index not ready"
			}
			writeError(w, http.StatusOK, id, -32000, msg, canon, true)
			return
		}

		writeResult(w, http.StatusOK, id, map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "Found results.",
				},
			},
			"structuredContent": map[string]interface{}{
				"query":             args.Query,
				"k":                 args.K,
				"hits":              hits,
				"indexing_complete": false,
			},
		})
	case "dir2mcp.open_file":
		var args openFileArgs
		if len(params.Arguments) > 0 {
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				writeError(w, http.StatusBadRequest, id, -32602, "invalid tool arguments", "INVALID_FIELD", false)
				return
			}
		}
		if strings.TrimSpace(args.RelPath) == "" {
			writeError(w, http.StatusBadRequest, id, -32602, "rel_path is required", "MISSING_FIELD", false)
			return
		}
		if args.MaxChars <= 0 {
			args.MaxChars = 20000
		}
		span := model.Span{}
		if args.Page > 0 {
			span = model.Span{Kind: "page", Page: args.Page}
		} else if args.StartMS > 0 || args.EndMS > 0 {
			span = model.Span{Kind: "time", StartMS: args.StartMS, EndMS: args.EndMS}
		} else if args.StartLine > 0 || args.EndLine > 0 {
			span = model.Span{Kind: "lines", StartLine: args.StartLine, EndLine: args.EndLine}
		}

		content, err := s.retriever.OpenFile(ctx, args.RelPath, span, args.MaxChars)
		if err != nil {
			log.Printf("tools/call open_file failed: id=%v tool=%s err=%v", id, params.Name, err)
			switch {
			case errors.Is(err, model.ErrForbidden):
				writeError(w, http.StatusOK, id, -32000, "forbidden", "FORBIDDEN", false)
			case errors.Is(err, model.ErrPathOutsideRoot):
				writeError(w, http.StatusOK, id, -32000, "path outside root", "PATH_OUTSIDE_ROOT", false)
			case errors.Is(err, model.ErrDocTypeUnsupported):
				writeError(w, http.StatusOK, id, -32000, "doc type unsupported", "DOC_TYPE_UNSUPPORTED", false)
			case errors.Is(err, os.ErrNotExist):
				writeError(w, http.StatusOK, id, -32000, "file not found", "NOT_FOUND", false)
			default:
				writeError(w, http.StatusOK, id, -32000, "internal server error", "INTERNAL_ERROR", true)
			}
			return
		}

		writeResult(w, http.StatusOK, id, map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": content,
				},
			},
			"structuredContent": map[string]interface{}{
				"rel_path":  args.RelPath,
				"doc_type":  inferDocType(args.RelPath),
				"span":      span,
				"content":   content,
				"truncated": len([]rune(content)) >= args.MaxChars,
			},
		})
	default:
		writeError(w, http.StatusBadRequest, id, -32602, "unknown tool", "INVALID_FIELD", false)
	}
}

func inferDocType(relPath string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(relPath)))
	switch ext {
	case ".go", ".js", ".ts", ".py", ".java", ".rb", ".cpp", ".c", ".cs":
		return "code"
	case ".md":
		return "md"
	case ".txt", ".rst":
		return "text"
	case ".pdf":
		return "pdf"
	case ".mp3", ".wav", ".m4a", ".flac":
		return "audio"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	default:
		return "unknown"
	}
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if strings.EqualFold(s.cfg.AuthMode, "none") {
		return true
	}

	expectedToken := s.authToken
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearerPrefix = "bearer "

	if len(authHeader) < len(bearerPrefix) || strings.ToLower(authHeader[:len(bearerPrefix)]) != bearerPrefix {
		writeError(w, http.StatusUnauthorized, nil, -32000, "missing or invalid bearer token", "UNAUTHORIZED", false)
		return false
	}

	providedToken := strings.TrimSpace(authHeader[len(bearerPrefix):])
	if expectedToken == "" || providedToken == "" {
		writeError(w, http.StatusUnauthorized, nil, -32000, "missing or invalid bearer token", "UNAUTHORIZED", false)
		return false
	}

	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken)) != 1 {
		writeError(w, http.StatusUnauthorized, nil, -32000, "missing or invalid bearer token", "UNAUTHORIZED", false)
		return false
	}

	return true
}

func (s *Server) allowOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	if isOriginAllowed(origin, s.cfg.AllowedOrigins) {
		return true
	}

	writeError(w, http.StatusForbidden, nil, -32000, "origin is not allowed", "FORBIDDEN_ORIGIN", false)
	return false
}

func parseRequest(body io.ReadCloser) (rpcRequest, error) {
	defer func() { _ = body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(body, maxRequestBody+1))
	if err != nil {
		return rpcRequest{}, err
	}
	if len(raw) > maxRequestBody {
		return rpcRequest{}, validationError{message: "request body too large", canonicalCode: "INVALID_FIELD"}
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return rpcRequest{}, validationError{message: "empty request body", canonicalCode: "MISSING_FIELD"}
	}
	if strings.HasPrefix(trimmed, "[") {
		return rpcRequest{}, validationError{message: "batch requests are not supported", canonicalCode: "INVALID_FIELD"}
	}

	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcRequest{}, validationError{message: "invalid json body", canonicalCode: "INVALID_FIELD"}
	}
	if req.JSONRPC != "2.0" {
		return rpcRequest{}, validationError{message: "jsonrpc must be \"2.0\"", canonicalCode: "INVALID_FIELD"}
	}
	return req, nil
}

func parseID(raw json.RawMessage) (interface{}, bool, error) {
	if raw == nil {
		return nil, false, nil
	}

	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, validationError{message: "invalid id", canonicalCode: "INVALID_FIELD"}
	}

	switch value.(type) {
	case nil, string, float64:
		return value, true, nil
	default:
		return nil, true, validationError{message: "id must be string, number, or null", canonicalCode: "INVALID_FIELD"}
	}
}

func writeResult(w http.ResponseWriter, statusCode int, id interface{}, result interface{}) {
	writeResponse(w, statusCode, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeError(w http.ResponseWriter, statusCode int, id interface{}, code int, message, canonicalCode string, retryable bool) {
	var errData *rpcErrorData
	if canonicalCode != "" {
		errData = &rpcErrorData{
			Code:      canonicalCode,
			Retryable: retryable,
		}
	}

	writeResponse(w, statusCode, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
			Data:    errData,
		},
	})
}

func writeResponse(w http.ResponseWriter, statusCode int, response rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) hasActiveSession(id string, now time.Time) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	lastSeen, ok := s.sessions[id]
	if !ok {
		return false
	}
	if now.Sub(lastSeen) > sessionTTL {
		delete(s.sessions, id)
		return false
	}

	s.sessions[id] = now
	return true
}

func (s *Server) storeSession(id string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessions[id] = time.Now()
}

func (s *Server) runSessionCleanup(ctx context.Context) {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.cleanupExpiredSessions(now)
		}
	}
}

func (s *Server) cleanupExpiredSessions(now time.Time) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	for id, lastSeen := range s.sessions {
		if now.Sub(lastSeen) > sessionTTL {
			delete(s.sessions, id)
		}
	}
}

func generateSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(b[:]), nil
}

func loadAuthToken(cfg config.Config) string {
	if token := strings.TrimSpace(cfg.ResolvedAuthToken); token != "" {
		return token
	}

	if token := strings.TrimSpace(os.Getenv(authTokenEnvVar)); token != "" {
		return token
	}

	path := filepath.Join(cfg.StateDir, "secret.token")
	content, err := os.ReadFile(path)
	if err != nil {
		// Log a warning so operators can diagnose missing tokens.  The
		// caller will still receive an empty string, which means no auth
		// token will be presented and certain AuthMode operations may be
		// blocked.
		log.Printf("warning: could not load auth token from %s: %v", path, err)
		return ""
	}
	return strings.TrimSpace(string(content))
}

func isOriginAllowed(origin string, allowlist []string) bool {
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return false
	}

	normalizedOrigin := parsedOrigin.Scheme + "://" + strings.ToLower(parsedOrigin.Host)
	originHost := strings.ToLower(parsedOrigin.Hostname())

	for _, allowed := range allowlist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}

		if strings.Contains(allowed, "://") {
			parsedAllowed, err := url.Parse(allowed)
			if err != nil || parsedAllowed.Scheme == "" || parsedAllowed.Host == "" {
				continue
			}
			if !strings.EqualFold(parsedAllowed.Scheme, parsedOrigin.Scheme) {
				continue
			}

			// Allow entries without an explicit port (e.g. http://localhost) to match any origin port.
			if parsedAllowed.Port() == "" {
				if strings.EqualFold(parsedAllowed.Hostname(), parsedOrigin.Hostname()) {
					return true
				}
				continue
			}

			normalizedAllowed := parsedAllowed.Scheme + "://" + strings.ToLower(parsedAllowed.Host)
			// both normalizedAllowed and normalizedOrigin have lowercase hosts and
			// scheme compared earlier using EqualFold, so a simple equality check
			// suffices here.
			if normalizedAllowed == normalizedOrigin {
				return true
			}
			continue
		}

		if strings.EqualFold(allowed, originHost) || strings.EqualFold(allowed, parsedOrigin.Host) {
			return true
		}
	}
	return false
}
