package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/config"
	"dir2mcp/internal/elevenlabs"
	"dir2mcp/internal/index"
	"dir2mcp/internal/ingest"
	"dir2mcp/internal/mcp"
	"dir2mcp/internal/mistral"
	"dir2mcp/internal/model"
	"dir2mcp/internal/retrieval"
	"dir2mcp/internal/store"
)

const (
	exitSuccess = iota
	exitGeneric
	exitConfigInvalid
	exitRootInaccessible
	exitServerBindFailure
	exitIndexLoadFailure
	exitIngestionFatal
)

const (
	authTokenEnvVar    = "DIR2MCP_AUTH_TOKEN"
	connectionFileName = "connection.json"
	secretTokenName    = "secret.token"
)

var commands = map[string]struct{}{
	"up":      {},
	"status":  {},
	"ask":     {},
	"reindex": {},
	"config":  {},
	"version": {},
}

type App struct {
	stdout io.Writer
	stderr io.Writer

	newIngestor func(config.Config, model.Store) model.Ingestor
}

type indexingStateAware interface {
	SetIndexingState(state *appstate.IndexingState)
}

type RuntimeHooks struct {
	NewIngestor func(config.Config, model.Store) model.Ingestor
}

type globalOptions struct {
	jsonOutput     bool
	nonInteractive bool
}

type upOptions struct {
	globalOptions
	readOnly            bool
	public              bool
	forceInsecure       bool
	x402ResourceBaseURL string
	auth                string
	listen              string
	mcpPath             string
	allowedOrigins      string
}

type authMaterial struct {
	mode              string
	token             string
	tokenSource       string
	tokenFile         string
	authorizationHint string
}

type connectionSession struct {
	UsesMCPSessionID     bool   `json:"uses_mcp_session_id"`
	HeaderName           string `json:"header_name"`
	AssignedOnInitialize bool   `json:"assigned_on_initialize"`
}

type connectionPayload struct {
	Transport   string            `json:"transport"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Session     connectionSession `json:"session"`
	Public      bool              `json:"public"`
	TokenSource string            `json:"token_source"`
	TokenFile   string            `json:"token_file,omitempty"`
}

type ndjsonEvent struct {
	Timestamp string      `json:"ts"`
	Level     string      `json:"level"`
	Event     string      `json:"event"`
	Data      interface{} `json:"data"`
}

type ndjsonEmitter struct {
	enabled bool
	out     io.Writer
}

func NewApp() *App {
	return NewAppWithIO(os.Stdout, os.Stderr)
}

func NewAppWithIO(stdout, stderr io.Writer) *App {
	return &App{
		stdout: stdout,
		stderr: stderr,
		newIngestor: func(cfg config.Config, st model.Store) model.Ingestor {
			svc := ingest.NewService(cfg, st)
			if strings.TrimSpace(cfg.MistralAPIKey) != "" {
				svc.SetOCR(mistral.NewClient(cfg.MistralBaseURL, cfg.MistralAPIKey))
			}
			return svc
		},
	}
}

func NewAppWithIOAndHooks(stdout, stderr io.Writer, hooks RuntimeHooks) *App {
	app := NewAppWithIO(stdout, stderr)
	if hooks.NewIngestor != nil {
		app.newIngestor = hooks.NewIngestor
	}
	return app
}

func writef(out io.Writer, format string, args ...interface{}) {
	_, _ = fmt.Fprintf(out, format, args...)
}

func writeln(out io.Writer, args ...interface{}) {
	_, _ = fmt.Fprintln(out, args...)
}

func (a *App) Run(args []string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return a.RunWithContext(ctx, args)
}

func (a *App) RunWithContext(ctx context.Context, args []string) int {
	if len(args) == 0 {
		a.printUsage()
		return exitSuccess
	}

	globalOpts, remaining, err := parseGlobalOptions(args)
	if err != nil {
		writef(a.stderr, "%v\n", err)
		return exitGeneric
	}
	if len(remaining) == 0 {
		a.printUsage()
		return exitSuccess
	}

	switch remaining[0] {
	case "up":
		upOpts, parseErr := parseUpOptions(globalOpts, remaining[1:])
		if parseErr != nil {
			writef(a.stderr, "invalid up flags: %v\n", parseErr)
			return exitConfigInvalid
		}
		return a.runUp(ctx, upOpts)
	case "status":
		return a.runStatus()
	case "ask":
		return a.runAsk(remaining[1:])
	case "reindex":
		return a.runReindex(ctx)
	case "config":
		return a.runConfig(remaining[1:])
	case "version":
		writeln(a.stdout, "dir2mcp skeleton v0.0.0-dev")
		return exitSuccess
	default:
		writef(a.stderr, "unknown command: %s\n", remaining[0])
		a.printUsage()
		return exitGeneric
	}
}

func (a *App) printUsage() {
	writeln(a.stdout, "dir2mcp skeleton")
	writeln(a.stdout, "usage: dir2mcp [--json] [--non-interactive] <command>")
	writeln(a.stdout, "commands: up, status, ask, reindex, config, version")
}

func (a *App) runUp(ctx context.Context, opts upOptions) int {
	cfg, err := config.Load(".dir2mcp.yaml")
	if err != nil {
		writef(a.stderr, "load config: %v\n", err)
		return exitConfigInvalid
	}

	if opts.listen != "" {
		cfg.ListenAddr = opts.listen
	}
	if opts.mcpPath != "" {
		cfg.MCPPath = opts.mcpPath
	}
	if opts.auth != "" {
		cfg.AuthMode = opts.auth
	}
	if opts.allowedOrigins != "" {
		cfg.AllowedOrigins = config.MergeAllowedOrigins(cfg.AllowedOrigins, opts.allowedOrigins)
	}
	if opts.public {
		cfg.Public = true

		// Public mode defaults to all interfaces unless operator provided --listen explicitly.
		if opts.listen == "" {
			port := "0"
			if _, parsedPort, splitErr := net.SplitHostPort(cfg.ListenAddr); splitErr == nil && parsedPort != "" {
				port = parsedPort
			}
			cfg.ListenAddr = net.JoinHostPort("0.0.0.0", port)
		}

		authMode := strings.TrimSpace(cfg.AuthMode)
		if strings.EqualFold(authMode, "none") && !opts.forceInsecure {
			writeln(a.stderr, "ERROR: CONFIG_INVALID: --public requires auth. Use --auth auto or --force-insecure to override (unsafe).")
			return exitConfigInvalid
		}
	}
	if !strings.HasPrefix(cfg.MCPPath, "/") {
		writeln(a.stderr, "CONFIG_INVALID: --mcp-path must start with '/'")
		return exitConfigInvalid
	}
	_ = opts.x402ResourceBaseURL

	if err := ensureRootAccessible(cfg.RootDir); err != nil {
		writef(a.stderr, "root inaccessible: %v\n", err)
		return exitRootInaccessible
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		writef(a.stderr, "create state dir: %v\n", err)
		return exitRootInaccessible
	}

	nonInteractiveMode := opts.nonInteractive || !isTerminal(os.Stdin) || !isTerminal(os.Stdout)
	if strings.TrimSpace(cfg.MistralAPIKey) == "" {
		if nonInteractiveMode {
			writeln(a.stderr, "ERROR: CONFIG_INVALID: Missing MISTRAL_API_KEY")
			writeln(a.stderr, "Set env: MISTRAL_API_KEY=...")
			writeln(a.stderr, "Or run: dir2mcp config init")
		} else {
			writeln(a.stderr, "CONFIG_INVALID: Missing MISTRAL_API_KEY")
			writeln(a.stderr, "Run: dir2mcp config init")
		}
		return exitConfigInvalid
	}

	auth, err := prepareAuthMaterial(cfg)
	if err != nil {
		writef(a.stderr, "auth setup: %v\n", err)
		return exitConfigInvalid
	}
	cfg.AuthMode = auth.mode
	cfg.ResolvedAuthToken = auth.token

	stateDB := filepath.Join(cfg.StateDir, "meta.sqlite")
	st := store.NewSQLiteStore(stateDB)
	defer func() {
		_ = st.Close()
	}()
	if err := st.Init(ctx); err != nil && !errors.Is(err, model.ErrNotImplemented) {
		writef(a.stderr, "initialize metadata store: %v\n", err)
		return exitIndexLoadFailure
	}

	ix := index.NewHNSWIndex(filepath.Join(cfg.StateDir, "vectors_text.hnsw"))
	defer func() {
		_ = ix.Close()
	}()
	if err := ix.Load(filepath.Join(cfg.StateDir, "vectors_text.hnsw")); err != nil &&
		!errors.Is(err, model.ErrNotImplemented) &&
		!errors.Is(err, os.ErrNotExist) {
		writef(a.stderr, "load index: %v\n", err)
		return exitIndexLoadFailure
	}

	client := mistral.NewClient(cfg.MistralBaseURL, cfg.MistralAPIKey)
	ret := retrieval.NewService(st, ix, client, client)
	ret.SetRootDir(cfg.RootDir)
	indexingState := appstate.NewIndexingState(appstate.ModeIncremental)

	serverOptions := []mcp.ServerOption{
		mcp.WithStore(st),
		mcp.WithIndexingState(indexingState),
	}
	if strings.TrimSpace(cfg.ElevenLabsAPIKey) != "" {
		ttsClient := elevenlabs.NewClient(cfg.ElevenLabsAPIKey, cfg.ElevenLabsTTSVoiceID)
		if strings.TrimSpace(cfg.ElevenLabsBaseURL) != "" {
			ttsClient.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.ElevenLabsBaseURL), "/")
		}
		serverOptions = append(serverOptions, mcp.WithTTS(ttsClient))
	}

	mcpServer := mcp.NewServer(cfg, ret, serverOptions...)
	ing := a.newIngestor(cfg, st)
	if stateAware, ok := ing.(indexingStateAware); ok {
		stateAware.SetIndexingState(indexingState)
	}

	emitter := newNDJSONEmitter(a.stdout, opts.jsonOutput)
	emitter.Emit("info", "index_loaded", map[string]interface{}{
		"state_dir": cfg.StateDir,
	})

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		writef(a.stderr, "bind server: %v\n", err)
		return exitServerBindFailure
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		_ = ln.Close()
	}()
	mcpAddr := ln.Addr().String()
	if cfg.Public {
		mcpAddr = publicURLAddress(cfg.ListenAddr, mcpAddr)
	}
	mcpURL := buildMCPURL(mcpAddr, cfg.MCPPath)

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- mcpServer.RunOnListener(runCtx, ln)
	}()

	emitter.Emit("info", "server_started", map[string]interface{}{
		"url":         mcpURL,
		"listen_addr": ln.Addr().String(),
		"public":      cfg.Public,
	})

	connection := buildConnectionPayload(cfg, mcpURL, auth)
	if err := writeConnectionFile(filepath.Join(cfg.StateDir, connectionFileName), connection); err != nil {
		writef(a.stderr, "write %s: %v\n", connectionFileName, err)
		return exitGeneric
	}

	emitter.Emit("info", "connection", connection)
	emitter.Emit("info", "scan_progress", map[string]interface{}{
		"scanned": 0,
		"indexed": 0,
		"skipped": 0,
		"deleted": 0,
		"reps":    0,
		"chunks":  0,
		"errors":  0,
	})
	emitter.Emit("info", "embed_progress", map[string]interface{}{
		"embedded": 0,
		"chunks":   0,
		"errors":   0,
	})

	if !opts.jsonOutput {
		a.printHumanConnection(cfg, connection, auth, opts.readOnly)
	}

	ingestErrCh := make(chan error, 1)
	if opts.readOnly {
		close(ingestErrCh)
	} else {
		go func() {
			defer close(ingestErrCh)
			// mode is already set at creation time; just mark running state
			indexingState.SetRunning(true)
			defer indexingState.SetRunning(false)
			runErr := ing.Run(runCtx)
			if errors.Is(runErr, model.ErrNotImplemented) {
				ingestErrCh <- nil
				return
			}
			ingestErrCh <- runErr
		}()
	}

	for {
		select {
		case <-runCtx.Done():
			return exitSuccess
		case serverErr := <-serverErrCh:
			if serverErr != nil {
				writef(a.stderr, "server failed: %v\n", serverErr)
				emitter.Emit("error", "fatal", map[string]interface{}{
					"code":    "SERVER_FAILURE",
					"message": serverErr.Error(),
				})
				return exitGeneric
			}
			return exitSuccess
		case ingestErr, ok := <-ingestErrCh:
			if !ok {
				ingestErrCh = nil
				continue
			}
			if ingestErr == nil {
				continue
			}
			writef(a.stderr, "ingestion failed: %v\n", ingestErr)
			emitter.Emit("error", "file_error", map[string]interface{}{
				"message": ingestErr.Error(),
			})
			emitter.Emit("error", "fatal", map[string]interface{}{
				"code":    "INGESTION_FATAL",
				"message": ingestErr.Error(),
			})
			return exitIngestionFatal
		}
	}
}

func (a *App) runStatus() int {
	writeln(a.stdout, "status command skeleton: not implemented")
	return exitSuccess
}

func (a *App) runAsk(args []string) int {
	if len(args) == 0 {
		writeln(a.stderr, "ask command requires a question argument")
		return exitGeneric
	}
	writef(a.stdout, "ask command skeleton: %q\n", args[0])
	return exitSuccess
}

func (a *App) runReindex(ctx context.Context) int {
	// load configuration first so that both the ingestor and any
	// auxiliary components (OCR client) share the same settings.  When
	// Load returns an error we treat it as fatal instead of silently
	// proceeding with defaults as was previously the case.
	cfg, err := config.Load(".dir2mcp.yaml")
	if err != nil {
		writef(a.stderr, "load config: %v\n", err)
		return exitConfigInvalid
	}

	baseDir := strings.TrimSpace(cfg.StateDir)
	if baseDir == "" {
		baseDir = ".dir2mcp"
	}
	st := store.NewSQLiteStore(filepath.Join(baseDir, "meta.sqlite"))
	defer func() {
		if closeErr := st.Close(); closeErr != nil {
			writef(a.stderr, "close store: %v\n", closeErr)
		}
	}()

	// use the factory hook (same as runUp) to allow tests to intercept
	ing := a.newIngestor(cfg, st)

	err = ing.Reindex(ctx)
	if errors.Is(err, model.ErrNotImplemented) {
		writeln(a.stdout, "reindex skeleton: ingestion pipeline not implemented yet")
		return exitSuccess
	}
	if err != nil {
		writef(a.stderr, "reindex failed: %v\n", err)
		return exitGeneric
	}
	return exitSuccess
}

func (a *App) runConfig(args []string) int {
	if len(args) == 0 {
		writeln(a.stdout, "config command skeleton: supported subcommands are init and print")
		return exitSuccess
	}
	switch args[0] {
	case "init":
		writeln(a.stdout, "config init skeleton: not implemented")
	case "print":
		cfg, err := config.Load(".dir2mcp.yaml")
		if err != nil {
			writef(a.stderr, "load config: %v\n", err)
			return exitConfigInvalid
		}
		writef(
			a.stdout,
			"root=%s state_dir=%s listen=%s mcp_path=%s mistral_base_url=%s mistral_api_key_set=%t\n",
			cfg.RootDir,
			cfg.StateDir,
			cfg.ListenAddr,
			cfg.MCPPath,
			cfg.MistralBaseURL,
			cfg.MistralAPIKey != "",
		)
	default:
		writef(a.stderr, "unknown config subcommand: %s\n", args[0])
		return exitGeneric
	}
	return exitSuccess
}

func parseGlobalOptions(args []string) (globalOptions, []string, error) {
	opts := globalOptions{}
	remaining := args

	for len(remaining) > 0 {
		arg := remaining[0]
		if _, ok := commands[arg]; ok {
			break
		}

		switch arg {
		case "--json":
			opts.jsonOutput = true
			remaining = remaining[1:]
		case "--non-interactive":
			opts.nonInteractive = true
			remaining = remaining[1:]
		default:
			if strings.HasPrefix(arg, "-") {
				return globalOptions{}, nil, fmt.Errorf("unknown global flag: %s", arg)
			}
			return opts, remaining, nil
		}
	}

	return opts, remaining, nil
}

func parseUpOptions(global globalOptions, args []string) (upOptions, error) {
	opts := upOptions{globalOptions: global}
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.jsonOutput, "json", opts.jsonOutput, "emit NDJSON events")
	fs.BoolVar(&opts.nonInteractive, "non-interactive", opts.nonInteractive, "disable prompts")
	fs.BoolVar(&opts.readOnly, "read-only", false, "run in read-only mode")
	fs.BoolVar(&opts.public, "public", false, "bind to all interfaces for external access")
	fs.BoolVar(&opts.forceInsecure, "force-insecure", false, "allow public mode without auth (unsafe)")
	fs.StringVar(&opts.x402ResourceBaseURL, "x402-resource-base-url", "", "x402 resource base URL")
	fs.StringVar(&opts.auth, "auth", "", "auth mode: auto|none|file:<path>")
	fs.StringVar(&opts.listen, "listen", "", "listen address")
	fs.StringVar(&opts.mcpPath, "mcp-path", "", "MCP route path")
	fs.StringVar(&opts.allowedOrigins, "allowed-origins", "", "comma-separated origins to append to the allowlist")
	if err := fs.Parse(args); err != nil {
		return upOptions{}, err
	}
	if fs.NArg() > 0 {
		return upOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func ensureRootAccessible(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", root)
	}
	return nil
}

func prepareAuthMaterial(cfg config.Config) (authMaterial, error) {
	mode := strings.TrimSpace(cfg.AuthMode)
	if mode == "" {
		mode = "auto"
	}

	if strings.EqualFold(mode, "none") {
		return authMaterial{
			mode:        "none",
			tokenSource: "none",
		}, nil
	}

	if strings.EqualFold(mode, "auto") {
		if token := strings.TrimSpace(os.Getenv(authTokenEnvVar)); token != "" {
			return authMaterial{
				mode:              "auto",
				token:             token,
				tokenSource:       "env",
				authorizationHint: "Bearer <token-from-env>",
			}, nil
		}

		tokenPath := filepath.Join(cfg.StateDir, secretTokenName)
		token, err := readToken(tokenPath, true)
		if err != nil {
			return authMaterial{}, err
		}
		if token == "" {
			token, err = generateTokenHex()
			if err != nil {
				return authMaterial{}, err
			}
			if err := writeSecretToken(tokenPath, token); err != nil {
				return authMaterial{}, err
			}
		}

		absPath := tokenPath
		if abs, err := filepath.Abs(tokenPath); err == nil {
			absPath = abs
		}
		return authMaterial{
			mode:              "auto",
			token:             token,
			tokenSource:       "secret.token",
			tokenFile:         absPath,
			authorizationHint: "Bearer <token-from-secret.token>",
		}, nil
	}

	if len(mode) >= len("file:") && strings.EqualFold(mode[:len("file:")], "file:") {
		tokenPath := strings.TrimSpace(mode[len("file:"):])
		if tokenPath == "" {
			return authMaterial{}, errors.New("auth mode file: requires a token path")
		}

		token, err := readToken(tokenPath, false)
		if err != nil {
			return authMaterial{}, err
		}
		if token == "" {
			return authMaterial{}, errors.New("auth file token is empty")
		}

		absPath := tokenPath
		if abs, err := filepath.Abs(tokenPath); err == nil {
			absPath = abs
		}
		return authMaterial{
			mode:              "file",
			token:             token,
			tokenSource:       "file",
			tokenFile:         absPath,
			authorizationHint: "Bearer <token-from-file>",
		}, nil
	}

	return authMaterial{}, fmt.Errorf("unsupported auth mode: %s", mode)
}

func readToken(path string, allowMissing bool) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func generateTokenHex() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeSecretToken(path, token string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := file.WriteString(token + "\n"); err != nil {
		return err
	}
	return nil
}

func buildMCPURL(addr, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "http://" + addr + path
}

// PublicURLAddress derives the public-facing address using the configured
// listen host and the resolved runtime port.
func PublicURLAddress(configuredListenAddr, resolvedListenAddr string) string {
	return publicURLAddress(configuredListenAddr, resolvedListenAddr)
}

func publicURLAddress(configuredListenAddr, resolvedListenAddr string) string {
	configuredListenAddr = strings.TrimSpace(configuredListenAddr)
	resolvedListenAddr = strings.TrimSpace(resolvedListenAddr)

	host := "0.0.0.0"
	if parsedHost, _, err := net.SplitHostPort(configuredListenAddr); err == nil && strings.TrimSpace(parsedHost) != "" {
		host = parsedHost
	}

	if port := extractPortFromAddress(resolvedListenAddr); port != "" {
		return net.JoinHostPort(host, port)
	}
	if port := extractPortFromAddress(configuredListenAddr); port != "" {
		return net.JoinHostPort(host, port)
	}

	return net.JoinHostPort(host, "0")
}

// ExtractPortFromAddress extracts a numeric trailing port token from a
// host:port address or malformed best-effort address string.
func ExtractPortFromAddress(addr string) string {
	return extractPortFromAddress(addr)
}

func extractPortFromAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}

	if _, port, err := net.SplitHostPort(addr); err == nil {
		port = strings.TrimSpace(port)
		if isNumericPort(port) {
			return port
		}
		return ""
	}

	// Best effort for malformed values where SplitHostPort fails but the
	// value still contains a trailing numeric ":port" token.
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return ""
	}
	port := addr[i+1:]
	if strings.ContainsAny(port, " \t\r\n/\\") {
		return ""
	}
	if isNumericPort(port) {
		return port
	}
	return ""
}

func isNumericPort(port string) bool {
	if port == "" {
		return false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func buildConnectionPayload(cfg config.Config, url string, auth authMaterial) connectionPayload {
	headers := map[string]string{
		"MCP-Protocol-Version": cfg.ProtocolVersion,
	}
	if auth.mode != "none" {
		headers["Authorization"] = auth.authorizationHint
	}

	return connectionPayload{
		Transport: "mcp_streamable_http",
		URL:       url,
		Headers:   headers,
		Session: connectionSession{
			UsesMCPSessionID:     true,
			HeaderName:           "MCP-Session-Id",
			AssignedOnInitialize: true,
		},
		Public:      cfg.Public,
		TokenSource: auth.tokenSource,
		TokenFile:   auth.tokenFile,
	}
}

func writeConnectionFile(path string, payload connectionPayload) error {
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

func newNDJSONEmitter(out io.Writer, enabled bool) *ndjsonEmitter {
	return &ndjsonEmitter{enabled: enabled, out: out}
}

func (e *ndjsonEmitter) Emit(level, event string, data interface{}) {
	if !e.enabled {
		return
	}
	entry := ndjsonEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Event:     event,
		Data:      data,
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(e.out, string(encoded))
}

func (a *App) printHumanConnection(cfg config.Config, connection connectionPayload, auth authMaterial, readOnly bool) {
	writef(a.stdout, "Index: %s\n", cfg.StateDir)
	mode := "incremental (server-first; indexing in background)"
	if readOnly {
		mode += ", read-only=true"
	}
	mode += fmt.Sprintf(", public=%t", cfg.Public)
	writef(a.stdout, "Mode: %s\n\n", mode)
	if cfg.Public {
		writeln(a.stdout, "WARNING: server is bound to all interfaces. Ensure auth is enabled.")
		writeln(a.stdout)
	}
	writeln(a.stdout, "MCP endpoint:")
	writef(a.stdout, "  URL:    %s\n", connection.URL)
	if auth.mode == "none" {
		writeln(a.stdout, "  Auth:   none")
	} else {
		writef(a.stdout, "  Auth:   Bearer (source=%s)\n", auth.tokenSource)
	}
	if auth.tokenFile != "" {
		writef(a.stdout, "  Token file: %s\n", auth.tokenFile)
	}
	writeln(a.stdout, "  Headers:")
	writef(a.stdout, "    MCP-Protocol-Version: %s\n", cfg.ProtocolVersion)
	if auth.mode != "none" {
		writeln(a.stdout, "    Authorization: Bearer <token>")
	}
	writeln(a.stdout, "    MCP-Session-Id: (assigned after initialize response)")
}

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
