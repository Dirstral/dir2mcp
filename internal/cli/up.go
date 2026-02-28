package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/mcp"
	"github.com/Dirstral/dir2mcp/internal/state"
	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start MCP server and run indexing in background",
	RunE:  runUp,
}

var (
	upListen        string
	upMcpPath       string
	upPublic        bool
	upAuth          string
	upTLSCert       string
	upTLSKey        string
	upX402          string
	upX402FacURL    string
	upX402Resource  string
	upX402Network   string
	upX402Price     string
	upReadOnly      bool
)

func init() {
	upCmd.Flags().StringVar(&upListen, "listen", "127.0.0.1:0", "host:port to listen on")
	upCmd.Flags().StringVar(&upMcpPath, "mcp-path", "/mcp", "HTTP path for MCP endpoint")
	upCmd.Flags().BoolVar(&upPublic, "public", false, "bind 0.0.0.0 and require token")
	upCmd.Flags().StringVar(&upAuth, "auth", "auto", "auth mode: auto|none|file:<path>")
	upCmd.Flags().StringVar(&upTLSCert, "tls-cert", "", "path to TLS cert")
	upCmd.Flags().StringVar(&upTLSKey, "tls-key", "", "path to TLS key")
	upCmd.Flags().StringVar(&upX402, "x402", "off", "x402 mode: off|on|required")
	upCmd.Flags().StringVar(&upX402FacURL, "x402-facilitator-url", "", "x402 facilitator URL")
	upCmd.Flags().StringVar(&upX402Resource, "x402-resource-base-url", "", "public base URL for payment requirements")
	upCmd.Flags().StringVar(&upX402Network, "x402-network", "", "x402 network id (e.g. eip155:8453)")
	upCmd.Flags().StringVar(&upX402Price, "x402-price", "", "default per-call price for paid routes")
	upCmd.Flags().BoolVar(&upReadOnly, "read-only", false, "read-only mode")
}

func runUp(cmd *cobra.Command, _ []string) error {
	rootDir, err := filepath.Abs(globalFlags.Dir)
	if err != nil {
		exitWith(ExitRootInaccessible, "ERROR: root directory inaccessible: "+err.Error())
	}
	info, err := os.Stat(rootDir)
	if err != nil || !info.IsDir() {
		exitWith(ExitRootInaccessible, "ERROR: root directory not found or not a directory: "+globalFlags.Dir)
	}

	stateDir := globalFlags.StateDir
	if stateDir == "" {
		stateDir = filepath.Join(rootDir, ".dir2mcp")
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		exitWith(ExitRootInaccessible, "ERROR: state directory path invalid: "+err.Error())
	}

	// Precedence: flags > env > file > defaults (issue #10)
	listenOverride := upListen
	if upPublic {
		listenOverride = "0.0.0.0:0"
	}
	overrides := &config.Overrides{
		ServerListen:  &listenOverride,
		ServerMCPPath: &upMcpPath,
		ServerPublic:  &upPublic,
	}
	cfg, err := config.Load(config.Options{
		ConfigPath:      globalFlags.ConfigPath,
		RootDir:         rootDir,
		StateDir:        stateDir,
		NonInteractive:  globalFlags.NonInteractive,
		JSON:            globalFlags.JSON,
		Overrides:       overrides,
	})
	if err != nil {
		exitWith(ExitConfigInvalid, "ERROR: "+err.Error())
	}
	cfg.Server.Auth = upAuth

	if err := state.EnsureStateDir(stateDir, cfg); err != nil {
		exitWith(ExitIndexLoadFailure, "ERROR: failed to init state: "+err.Error())
	}
	lockPath := filepath.Join(stateDir, "locks", "index.lock")
	defer os.Remove(lockPath)

	listenAddr := cfg.Server.Listen
	if upPublic && listenAddr == "0.0.0.0:0" {
		listenAddr = "0.0.0.0:0"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		exitWith(ExitBindFailure, "ERROR: server bind failure: "+err.Error())
	}
	addr := listener.Addr().(*net.TCPAddr)
	baseURL := fmt.Sprintf("http://%s", addr.String())
	if upTLSCert != "" && upTLSKey != "" {
		baseURL = fmt.Sprintf("https://%s", addr.String())
	}
	mcpURL := baseURL + upMcpPath

	token, tokenSource, err := state.ResolveAuthToken(stateDir, upAuth)
	if err != nil {
		exitWith(ExitConfigInvalid, "ERROR: auth: "+err.Error())
	}

	if err := state.WriteConnectionJSON(stateDir, mcpURL, token, tokenSource, upAuth); err != nil {
		exitWith(ExitIndexLoadFailure, "ERROR: failed to write connection.json: "+err.Error())
	}

	jobID := "job_" + time.Now().UTC().Format("20060102-150405")
	corpus := state.InitialCorpus(rootDir, jobID, cfg)
	if err := state.WriteCorpusJSON(stateDir, corpus); err != nil {
		exitWith(ExitIndexLoadFailure, "ERROR: failed to write corpus.json: "+err.Error())
	}

	if !globalFlags.Quiet && !globalFlags.JSON {
		fmt.Println("Index:", stateDir, " (meta.sqlite + vectors_text.hnsw + vectors_code.hnsw)")
		fmt.Println("Mode: incremental  (server-first; indexing in background)")
		fmt.Println()
		fmt.Println("MCP endpoint:")
		fmt.Println("  URL:   ", mcpURL)
		fmt.Println("  Auth:  ", "Bearer (source="+tokenSource+")")
		fmt.Println("  Headers:")
		fmt.Println("    MCP-Protocol-Version: 2025-11-25")
		fmt.Println("    Authorization: Bearer <token>")
		fmt.Println("    MCP-Session-Id: (assigned after initialize response)")
		fmt.Println()
		fmt.Println("Web UI: cd ui && NEXT_PUBLIC_API_URL=" + baseURL + " npm run dev")
		fmt.Println()
	}

	server, err := mcp.NewServer(mcp.ServerOptions{
		RootDir:    rootDir,
		StateDir:   stateDir,
		Config:     cfg,
		McpPath:    upMcpPath,
		AuthToken:  token,
	})
	if err != nil {
		exitWith(ExitIndexLoadFailure, "ERROR: MCP server init: "+err.Error())
	}

	// Start background indexer (incremental)
	ctx := context.Background()
	go server.RunIndexer(ctx)

	if globalFlags.JSON {
		emitNDJSON("server_started", map[string]interface{}{"url": mcpURL})
		emitNDJSON("connection", map[string]interface{}{
			"transport":    "mcp_streamable_http",
			"url":          mcpURL,
			"token_source": tokenSource,
		})
	}

	// Combined mux: MCP path + /api/mcp proxy (for web UI) + /api/corpus
	mux := http.NewServeMux()
	mux.Handle(upMcpPath, server.MCPHandler())
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		proxyToMCP(w, r, mcpURL, token)
	})
	mux.HandleFunc("/api/corpus", func(w http.ResponseWriter, r *http.Request) {
		serveCorpusJSON(w, r, stateDir)
	})
	allowedOrigins := cfg.Security.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	}
	handler := corsForAPI(mux, allowedOrigins)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv.Serve(listener)
}

// corsForAPI adds CORS headers for /api/* when Origin is in allowed list.
func corsForAPI(next http.Handler, allowedOrigins []string) http.Handler {
	allowed := make(map[string]bool)
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			origin := r.Header.Get("Origin")
			if allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// proxyToMCP forwards the request to mcpURL with Authorization: Bearer token.
func proxyToMCP(w http.ResponseWriter, r *http.Request, mcpURL, token string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, mcpURL, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// serveCorpusJSON serves GET /api/corpus with stateDir/corpus.json.
func serveCorpusJSON(w http.ResponseWriter, r *http.Request, stateDir string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "corpus.json"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "corpus.json not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func emitNDJSON(event string, data map[string]interface{}) {
	// NDJSON: one JSON object per line (SPEC §3.2)
	out := map[string]interface{}{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"level": "info",
		"event": event,
		"data":  data,
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(out)
}
