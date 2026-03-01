package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
	"dir2mcp/internal/x402"
)

const (
	sessionHeaderName      = "MCP-Session-Id"
	authTokenEnvVar        = "DIR2MCP_AUTH_TOKEN"
	maxRequestBody         = 1 << 20
	sessionTTL             = 24 * time.Hour
	sessionCleanupInterval = time.Hour
	rateLimitCleanupEvery  = 5 * time.Minute
	rateLimitBucketMaxAge  = 10 * time.Minute
	// Replay outcomes are only useful while signatures are still valid.
	// Keep a small buffer over common short-lived signature windows.
	paymentOutcomeTTL             = 10 * time.Minute
	paymentOutcomeCleanupInterval = time.Minute
	paymentOutcomeMaxEntries      = 5000
)

// DefaultSearchK is used when tools/call search arguments omit k or provide
// a non-positive value.
const DefaultSearchK = 10

type Server struct {
	cfg       config.Config
	authToken string
	retriever model.Retriever
	store     model.Store
	indexing  *appstate.IndexingState
	tts       TTSSynthesizer
	tools     map[string]toolDefinition

	sessionMu sync.RWMutex
	sessions  map[string]time.Time

	rateLimiter *ipRateLimiter

	x402Client      *x402.HTTPClient
	x402Requirement x402.Requirement
	x402Enabled     bool
	paymentLogPath  string
	paymentMu       sync.RWMutex
	paymentOutcomes map[string]paymentExecutionOutcome
	paymentTTL      time.Duration
	paymentMaxItems int

	// cached writer used by appendPaymentLog. protected by paymentLogMu.
	paymentLogMu     sync.Mutex
	paymentLogFile   *os.File
	paymentLogWriter *bufio.Writer

	// per-execution-key locks used to serialize payment handling for identical
	// signatures+params.  Map is protected by execMu.
	execMu    sync.Mutex
	execKeyMu map[string]*keyMutex

	eventEmitter func(level, event string, data interface{})
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

func (e validationError) Error() string {
	return e.message
}

type ServerOption func(*Server)

type TTSSynthesizer interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

func WithStore(store model.Store) ServerOption {
	return func(s *Server) {
		s.store = store
	}
}

func WithIndexingState(state *appstate.IndexingState) ServerOption {
	return func(s *Server) {
		s.indexing = state
	}
}

func WithTTS(tts TTSSynthesizer) ServerOption {
	return func(s *Server) {
		s.tts = tts
	}
}

func WithEventEmitter(fn func(level, event string, data interface{})) ServerOption {
	return func(s *Server) {
		s.eventEmitter = fn
	}
}

func NewServer(cfg config.Config, retriever model.Retriever, opts ...ServerOption) *Server {
	s := &Server{
		cfg:             cfg,
		authToken:       loadAuthToken(cfg),
		retriever:       retriever,
		sessions:        make(map[string]time.Time),
		paymentOutcomes: make(map[string]paymentExecutionOutcome),
		paymentTTL:      paymentOutcomeTTL,
		paymentMaxItems: paymentOutcomeMaxEntries,
		execKeyMu:       make(map[string]*keyMutex),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if s.indexing == nil {
		s.indexing = appstate.NewIndexingState(appstate.ModeIncremental)
	}
	if cfg.Public && cfg.RateLimitRPS > 0 && cfg.RateLimitBurst > 0 {
		s.rateLimiter = newIPRateLimiter(float64(cfg.RateLimitRPS), cfg.RateLimitBurst, cfg.TrustedProxies)
	}
	s.initPaymentConfig()
	s.tools = s.buildToolRegistry()
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.MCPPath, s.handleMCP)
	return s.corsMiddleware(mux)
}

// corsMiddleware wraps the handler to support CORS preflight (OPTIONS) and
// response headers for the MCP endpoint. Required for browser-based MCP
// clients such as ElevenLabs Conversational AI.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && isOriginAllowed(origin, s.cfg.AllowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version, MCP-Session-Id, PAYMENT-SIGNATURE")
			w.Header().Set("Access-Control-Expose-Headers", "MCP-Session-Id, PAYMENT-REQUIRED, PAYMENT-RESPONSE")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.Header().Set("Vary", "Origin")
		}

		accessControlRequestMethod := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method"))
		accessControlRequestHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
		isPreflight := r.Method == http.MethodOptions &&
			origin != "" &&
			(accessControlRequestMethod != "" || accessControlRequestHeaders != "")
		if isPreflight {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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
	// make sure any cached payment-log resources are flushed when the server
	// stops; the deferred call is harmless if nothing was opened.
	defer func() { _ = s.Close() }() // ignore error per errcheck

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.runSessionCleanup(runCtx)
	if s.rateLimiter != nil {
		go s.runRateLimitCleanup(runCtx)
	}
	if s.x402Enabled {
		go s.runPaymentOutcomeCleanup(runCtx)
	}

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
	if s.rateLimiter != nil {
		if !s.rateLimiter.allow(realIP(r, s.rateLimiter)) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, nil, -32000, "rate limit exceeded", "RATE_LIMIT_EXCEEDED", true)
			return
		}
	}

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
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		s.handleToolsList(w, id)
	case "tools/call":
		if !hasID {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		s.handleToolsCallRequest(r.Context(), w, r, req.Params, id)
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

func (s *Server) runRateLimitCleanup(ctx context.Context) {
	ticker := time.NewTicker(rateLimitCleanupEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.rateLimiter.cleanup(rateLimitBucketMaxAge)
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

func (s *Server) runPaymentOutcomeCleanup(ctx context.Context) {
	ticker := time.NewTicker(paymentOutcomeCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.cleanupPaymentOutcomes(now)
		}
	}
}

func (s *Server) cleanupPaymentOutcomes(now time.Time) {
	s.paymentMu.Lock()
	defer s.paymentMu.Unlock()
	s.prunePaymentOutcomesLocked(now)
}

func (s *Server) prunePaymentOutcomesLocked(now time.Time) {
	ttl := s.paymentTTL
	if ttl <= 0 {
		ttl = paymentOutcomeTTL
	}

	cutoff := now.Add(-ttl)
	for key, outcome := range s.paymentOutcomes {
		if outcome.UpdatedAt.IsZero() || outcome.UpdatedAt.Before(cutoff) {
			delete(s.paymentOutcomes, key)
		}
	}

	maxItems := s.paymentMaxItems
	if maxItems <= 0 {
		maxItems = paymentOutcomeMaxEntries
	}
	if len(s.paymentOutcomes) <= maxItems {
		return
	}

	type entry struct {
		key string
		ts  time.Time
	}
	entries := make([]entry, 0, len(s.paymentOutcomes))
	for key, outcome := range s.paymentOutcomes {
		entries = append(entries, entry{key: key, ts: outcome.UpdatedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ts.Before(entries[j].ts)
	})

	toDrop := len(entries) - maxItems
	for i := 0; i < toDrop; i++ {
		delete(s.paymentOutcomes, entries[i].key)
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
			if strings.EqualFold(normalizedAllowed, normalizedOrigin) {
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

// Close flushes and closes any cached payment log writer and file.
//
// The method is safe to call multiple times (idempotent) and is typically
// invoked during server shutdown to guarantee that any buffered payments are
// persisted. It acquires the paymentLogMu mutex, clears
// s.paymentLogWriter and s.paymentLogFile under that lock, and returns a
// combined error using errors.Join if flushing or closing fails.
func (s *Server) Close() error {
	s.paymentLogMu.Lock()
	defer s.paymentLogMu.Unlock()

	var errs []error
	if s.paymentLogWriter != nil {
		if err := s.paymentLogWriter.Flush(); err != nil {
			errs = append(errs, fmt.Errorf("payment log flush: %w", err))
		}
		s.paymentLogWriter = nil
	}
	if s.paymentLogFile != nil {
		if err := s.paymentLogFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("payment log file close: %w", err))
		}
		s.paymentLogFile = nil
	}
	// errors.Join will return nil when "errs" is empty, which keeps behavior
	// consistent with the previous implementation.
	return errors.Join(errs...)
}
