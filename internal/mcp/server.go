package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
	sessionHeaderName = "MCP-Session-Id"
	authTokenEnvVar   = "DIR2MCP_AUTH_TOKEN"
	maxRequestBody    = 1 << 20
	sessionTTL        = 24 * time.Hour
	sessionCleanupInterval = time.Hour
)

type Server struct {
	cfg       config.Config
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

func NewServer(cfg config.Config, retriever model.Retriever) *Server {
	return &Server{
		cfg:       cfg,
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
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.runSessionCleanup(runCtx)

	server := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: s.Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
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
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, nil, -32600, "Content-Type must be application/json", "INVALID_CONTENT_TYPE", false)
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
		writeError(w, http.StatusBadRequest, nil, -32600, parseErr.Error(), "INVALID_REQUEST", false)
		return
	}

	id := idValue(req.ID)

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
		s.handleInitialize(w, id)
	case "notifications/initialized":
		if id == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeResult(w, http.StatusOK, id, map[string]interface{}{})
	default:
		if id == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeError(w, http.StatusOK, id, -32601, "method not found", "METHOD_NOT_FOUND", false)
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, id interface{}) {
	if id == nil {
		writeError(w, http.StatusBadRequest, nil, -32600, "initialize requires id", "INVALID_REQUEST", false)
		return
	}

	sessionID, err := generateSessionID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, id, -32603, "failed to initialize session", "INTERNAL", false)
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

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if strings.EqualFold(s.cfg.AuthMode, "none") {
		return true
	}

	expectedToken := loadAuthToken(s.cfg)
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
	defer body.Close()

	raw, err := io.ReadAll(io.LimitReader(body, maxRequestBody+1))
	if err != nil {
		return rpcRequest{}, err
	}
	if len(raw) > maxRequestBody {
		return rpcRequest{}, errors.New("request body too large")
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return rpcRequest{}, errors.New("empty request body")
	}
	if strings.HasPrefix(trimmed, "[") {
		return rpcRequest{}, errors.New("batch requests are not supported")
	}

	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcRequest{}, err
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return rpcRequest{}, errors.New("unsupported jsonrpc version")
	}
	return req, nil
}

func idValue(raw json.RawMessage) interface{} {
	if raw == nil {
		return nil
	}

	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func writeResult(w http.ResponseWriter, statusCode int, id interface{}, result interface{}) {
	writeResponse(w, statusCode, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeError(w http.ResponseWriter, statusCode int, id interface{}, code int, message, canonicalCode string, retryable bool) {
	writeResponse(w, statusCode, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
			Data: &rpcErrorData{
				Code:      canonicalCode,
				Retryable: retryable,
			},
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
	if token := strings.TrimSpace(os.Getenv(authTokenEnvVar)); token != "" {
		return token
	}

	path := filepath.Join(cfg.StateDir, "secret.token")
	content, err := os.ReadFile(path)
	if err != nil {
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
			normalizedAllowed := parsedAllowed.Scheme + "://" + strings.ToLower(parsedAllowed.Host)
			if strings.EqualFold(normalizedAllowed, normalizedOrigin) {
				return true
			}
			continue
		}

		if strings.EqualFold(strings.ToLower(allowed), originHost) || strings.EqualFold(strings.ToLower(allowed), strings.ToLower(parsedOrigin.Host)) {
			return true
		}
	}
	return false
}
