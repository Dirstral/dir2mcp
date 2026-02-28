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
	"log"
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
	newStore    func(config.Config) model.Store
}

type indexingStateAware interface {
	SetIndexingState(state *appstate.IndexingState)
}

type contentHashResetter interface {
	ClearDocumentContentHashes(ctx context.Context) error
}

type embeddedChunkLister interface {
	ListEmbeddedChunkMetadata(ctx context.Context, indexKind string, limit, offset int) ([]model.ChunkTask, error)
}

type activeDocCountStore interface {
	ActiveDocCounts(ctx context.Context) (map[string]int64, int64, error)
}

type RuntimeHooks struct {
	NewIngestor func(config.Config, model.Store) model.Ingestor
	NewStore    func(config.Config) model.Store
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
	// embed model overrides, set via flags or env/config
	embedModelText string
	embedModelCode string
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

type corpusSnapshot struct {
	Timestamp    string           `json:"ts"`
	Indexing     corpusIndexing   `json:"indexing"`
	DocCounts    map[string]int64 `json:"doc_counts"`
	TotalDocs    int64            `json:"total_docs"`
	CodeRatio    float64          `json:"code_ratio"`
	CacheableFor string           `json:"cacheable_for,omitempty"`
}

type corpusIndexing struct {
	Mode            string `json:"mode"`
	Running         bool   `json:"running"`
	Scanned         int64  `json:"scanned"`
	Indexed         int64  `json:"indexed"`
	Skipped         int64  `json:"skipped"`
	Deleted         int64  `json:"deleted"`
	Representations int64  `json:"representations"`
	ChunksTotal     int64  `json:"chunks_total"`
	EmbeddedOK      int64  `json:"embedded_ok"`
	Errors          int64  `json:"errors"`
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
		// default store constructor uses sqlite in the configured state
		// directory.  tests can override via RuntimeHooks.NewStore.
		newStore: func(cfg config.Config) model.Store {
			return store.NewSQLiteStore(filepath.Join(cfg.StateDir, "meta.sqlite"))
		},
	}
}

func NewAppWithIOAndHooks(stdout, stderr io.Writer, hooks RuntimeHooks) *App {
	app := NewAppWithIO(stdout, stderr)
	if hooks.NewIngestor != nil {
		app.newIngestor = hooks.NewIngestor
	}
	if hooks.NewStore != nil {
		app.newStore = hooks.NewStore
	}
	return app
}

func writef(out io.Writer, format string, args ...interface{}) {
	_, _ = fmt.Fprintf(out, format, args...)
}

func writeln(out io.Writer, args ...interface{}) {
	_, _ = fmt.Fprintln(out, args...)
}

func (a *App) storeForConfig(cfg config.Config) model.Store {
	if a != nil && a.newStore != nil {
		return a.newStore(cfg)
	}
	return store.NewSQLiteStore(filepath.Join(cfg.StateDir, "meta.sqlite"))
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
	writeln(a.stdout, "for 'up' the following flags are available: --listen, --mcp-path, --public, --read-only, --auth, --allowed-origins, --embed-model-text, --embed-model-code, ...")
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
	if opts.embedModelText != "" {
		cfg.EmbedModelText = opts.embedModelText
	}
	if opts.embedModelCode != "" {
		cfg.EmbedModelCode = opts.embedModelCode
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

	st := a.storeForConfig(cfg)
	defer func() { _ = st.Close() }()
	if err := st.Init(ctx); err != nil && !errors.Is(err, model.ErrNotImplemented) {
		writef(a.stderr, "initialize metadata store: %v\n", err)
		return exitIndexLoadFailure
	}

	textIndexPath := filepath.Join(cfg.StateDir, "vectors_text.hnsw")
	codeIndexPath := filepath.Join(cfg.StateDir, "vectors_code.hnsw")

	textIx := index.NewHNSWIndex(textIndexPath)
	defer func() {
		_ = textIx.Close()
	}()
	if err := textIx.Load(textIndexPath); err != nil &&
		!errors.Is(err, model.ErrNotImplemented) &&
		!errors.Is(err, os.ErrNotExist) {
		writef(a.stderr, "load text index: %v\n", err)
		return exitIndexLoadFailure
	}

	codeIx := index.NewHNSWIndex(codeIndexPath)
	defer func() {
		_ = codeIx.Close()
	}()
	if err := codeIx.Load(codeIndexPath); err != nil &&
		!errors.Is(err, model.ErrNotImplemented) &&
		!errors.Is(err, os.ErrNotExist) {
		writef(a.stderr, "load code index: %v\n", err)
		return exitIndexLoadFailure
	}

	client := mistral.NewClient(cfg.MistralBaseURL, cfg.MistralAPIKey)
	ret := retrieval.NewService(st, textIx, client, client)
	ret.SetCodeIndex(codeIx)
	ret.SetRootDir(cfg.RootDir)

	// events are emitted to stdout only after we create the emitter; moving
	// creation before the preload call lets us report failures from that
	// bootstrap step as structured events (see SPEC.md for NDJSON schema).
	emitter := newNDJSONEmitter(a.stdout, opts.jsonOutput)

	preloadedChunks := 0
	if metadataStore, ok := st.(embeddedChunkLister); ok {
		preloadedChunks, err = preloadEmbeddedChunkMetadata(ctx, metadataStore, ret)
		if err != nil {
			// surface the problem in both stderr and the NDJSON event stream so
			// automation can detect a bootstrap warning.
			writef(a.stderr, "bootstrap embedded chunk metadata: %v\n", err)
			emitter.Emit("warning", "bootstrap_embedded_chunk_metadata", map[string]interface{}{
				"message": err.Error(),
			})
		}
	}
	indexingState := appstate.NewIndexingState(appstate.ModeIncremental)
	if preloadedChunks > 0 {
		indexingState.AddEmbeddedOK(int64(preloadedChunks))
	}
	ret.SetIndexingCompleteProvider(func() bool {
		return !indexingState.Snapshot().Running
	})

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
	persistence := index.NewPersistenceManager(
		[]index.IndexedFile{
			{Path: textIndexPath, Index: textIx},
			{Path: codeIndexPath, Index: codeIx},
		},
		15*time.Second,
		func(saveErr error) { writef(a.stderr, "index autosave warning: %v\n", saveErr) },
	)
	persistence.Start(runCtx)
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		if stopErr := persistence.StopAndSave(stopCtx); stopErr != nil && !errors.Is(stopErr, context.Canceled) {
			writef(a.stderr, "final index save warning: %v\n", stopErr)
		}
	}()

	embedErrCh := make(chan error, 4)
	if !opts.readOnly {
		if chunkSource, ok := st.(index.ChunkSource); ok {
			// choose an embed worker logger appropriate for JSON mode so that
			// unstructured log output never leaks into the NDJSON stream.  when
			// in JSON mode we simply discard logs; otherwise forward to the CLI
			// stderr writer (which tests can capture).
			var embedLogger *log.Logger
			if opts.jsonOutput {
				embedLogger = log.New(io.Discard, "", 0)
			} else {
				embedLogger = log.New(a.stderr, "", log.LstdFlags)
			}
			startEmbeddingWorkers(runCtx, chunkSource, textIx, codeIx, client, ret, indexingState, embedErrCh, embedLogger, cfg.EmbedModelText, cfg.EmbedModelCode)
		}
	}
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
	go runCorpusWriter(runCtx, cfg.StateDir, st, indexingState, a.stderr)

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
				_ = writeCorpusSnapshot(runCtx, cfg.StateDir, st, indexingState)
				continue
			}
			if ingestErr == nil {
				_ = writeCorpusSnapshot(runCtx, cfg.StateDir, st, indexingState)
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
		case embedErr := <-embedErrCh:
			if embedErr == nil {
				continue
			}
			writef(a.stderr, "embedding worker warning: %v\n", embedErr)
			emitter.Emit("error", "embed_error", map[string]interface{}{
				"message": embedErr.Error(),
			})
		}
	}
}

func preloadEmbeddedChunkMetadata(ctx context.Context, source embeddedChunkLister, ret *retrieval.Service) (int, error) {
	if source == nil || ret == nil {
		return 0, nil
	}
	const pageSize = 500
	total := 0
	kinds := []string{"text", "code"}
	for _, kind := range kinds {
		offset := 0
		for {
			tasks, err := source.ListEmbeddedChunkMetadata(ctx, kind, pageSize, offset)
			if err != nil {
				if errors.Is(err, model.ErrNotImplemented) {
					break
				}
				return total, err
			}
			for _, task := range tasks {
				ret.SetChunkMetadataForIndex(kind, task.Metadata.ChunkID, model.SearchHit{
					ChunkID: task.Metadata.ChunkID,
					RelPath: task.Metadata.RelPath,
					DocType: task.Metadata.DocType,
					RepType: task.Metadata.RepType,
					Snippet: task.Metadata.Snippet,
					Span:    task.Metadata.Span,
				})
				total++
			}
			if len(tasks) < pageSize {
				break
			}
			offset += len(tasks)
		}
	}
	return total, nil
}

func startEmbeddingWorkers(
	ctx context.Context,
	st index.ChunkSource,
	textIndex model.Index,
	codeIndex model.Index,
	embedder model.Embedder,
	ret *retrieval.Service,
	indexingState *appstate.IndexingState,
	errCh chan<- error,
	logger *log.Logger,
	textModel, codeModel string,
) {
	if st == nil || embedder == nil {
		return
	}

	start := func(kind string, ix model.Index) {
		if ix == nil {
			return
		}
		workerKind := kind
		worker := &index.EmbeddingWorker{
			Source:       st,
			Index:        ix,
			Embedder:     embedder,
			ModelForText: textModel,
			ModelForCode: codeModel,
			BatchSize:    32,
			Logger:       logger,
			OnIndexedChunk: func(label uint64, metadata model.ChunkMetadata) {
				if ret != nil {
					ret.SetChunkMetadataForIndex(workerKind, label, model.SearchHit{
						ChunkID: metadata.ChunkID,
						RelPath: metadata.RelPath,
						DocType: metadata.DocType,
						RepType: metadata.RepType,
						Snippet: metadata.Snippet,
						Span:    metadata.Span,
					})
				}
				if indexingState != nil {
					indexingState.AddEmbeddedOK(1)
				}
			},
		}

		go func() {
			err := worker.Run(ctx, 750*time.Millisecond, workerKind)
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			errCh <- fmt.Errorf("%s worker: %w", workerKind, err)
		}()
	}

	start("text", textIndex)
	start("code", codeIndex)
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
	// ensure the directory exists before we let the store constructor write
	// to it
	textIndexPath := filepath.Join(baseDir, "vectors_text.hnsw")
	codeIndexPath := filepath.Join(baseDir, "vectors_code.hnsw")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		writef(a.stderr, "create state dir: %v\n", err)
		return exitRootInaccessible
	}
	// update cfg so that the store factory uses the same baseDir
	cfg.StateDir = baseDir
	st := a.storeForConfig(cfg)
	defer func() {
		if closeErr := st.Close(); closeErr != nil {
			writef(a.stderr, "close store: %v\n", closeErr)
		}
	}()
	if err := st.Init(ctx); err != nil && !errors.Is(err, model.ErrNotImplemented) {
		writef(a.stderr, "initialize metadata store: %v\n", err)
		return exitIndexLoadFailure
	}
	if resetter, ok := interface{}(st).(contentHashResetter); ok {
		if err := resetter.ClearDocumentContentHashes(ctx); err != nil {
			writef(a.stderr, "clear content hashes: %v\n", err)
			return exitGeneric
		}
	}
	for _, indexPath := range []string{textIndexPath, codeIndexPath} {
		if err := os.Remove(indexPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			writef(a.stderr, "remove stale index file %s: %v\n", indexPath, err)
			return exitGeneric
		}
	}

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
	fs.StringVar(&opts.embedModelText, "embed-model-text", "", "override embedding model used for text chunks")
	fs.StringVar(&opts.embedModelCode, "embed-model-code", "", "override embedding model used for code chunks")
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

func runCorpusWriter(ctx context.Context, stateDir string, st model.Store, indexingState *appstate.IndexingState, stderr io.Writer) {
	runCorpusWriterWithInterval(ctx, stateDir, st, indexingState, stderr, 5*time.Second)
}

func runCorpusWriterWithInterval(ctx context.Context, stateDir string, st model.Store, indexingState *appstate.IndexingState, stderr io.Writer, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	// Emit an initial snapshot immediately, then refresh while indexing runs.
	if err := writeCorpusSnapshot(ctx, stateDir, st, indexingState); err != nil {
		writef(stderr, "write corpus snapshot: %v\n", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if indexingState != nil && !indexingState.Snapshot().Running {
				continue
			}
			if err := writeCorpusSnapshot(ctx, stateDir, st, indexingState); err != nil {
				writef(stderr, "write corpus snapshot: %v\n", err)
			}
		}
	}
}

func writeCorpusSnapshot(ctx context.Context, stateDir string, st model.Store, indexingState *appstate.IndexingState) error {
	snapshot, err := buildCorpusSnapshot(ctx, st, indexingState)
	if err != nil {
		return err
	}

	path := filepath.Join(stateDir, "corpus.json")
	// Use a per-write temporary file so concurrent snapshot writers don't
	// stomp each other's tmp file and trigger spurious ENOENT on rename.
	tmpFile, err := os.CreateTemp(stateDir, "corpus.json.tmp.")
	if err != nil {
		return fmt.Errorf("create temp corpus snapshot: %w", err)
	}
	tmp := tmpFile.Name()

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("marshal corpus snapshot: %w", err)
	}

	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write temp corpus snapshot: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp corpus snapshot: %w", err)
	}
	// Match previous file mode (0o644) used with os.WriteFile.
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod temp corpus snapshot: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// os.Rename fails on Windows when the destination already exists.
		// Remove the existing file and retry once to support Windows.
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("rename corpus snapshot: %w", err2)
		}
	}
	return nil
}

func buildCorpusSnapshot(ctx context.Context, st model.Store, indexingState *appstate.IndexingState) (corpusSnapshot, error) {
	docCounts, totalDocs, err := collectActiveDocCounts(ctx, st)
	if err != nil {
		return corpusSnapshot{}, err
	}

	codeDocs := docCounts["code"]
	codeRatio := 0.0
	if totalDocs > 0 {
		codeRatio = float64(codeDocs) / float64(totalDocs)
	}

	idx := appstate.IndexingSnapshot{Mode: appstate.ModeIncremental}
	if indexingState != nil {
		idx = indexingState.Snapshot()
	}

	return corpusSnapshot{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Indexing: corpusIndexing{
			Mode:            idx.Mode,
			Running:         idx.Running,
			Scanned:         idx.Scanned,
			Indexed:         idx.Indexed,
			Skipped:         idx.Skipped,
			Deleted:         idx.Deleted,
			Representations: idx.Representations,
			ChunksTotal:     idx.ChunksTotal,
			EmbeddedOK:      idx.EmbeddedOK,
			Errors:          idx.Errors,
		},
		DocCounts: docCounts,
		TotalDocs: totalDocs,
		CodeRatio: codeRatio,
	}, nil
}

func collectActiveDocCounts(ctx context.Context, st model.Store) (map[string]int64, int64, error) {
	if st == nil {
		return map[string]int64{}, 0, nil
	}
	if agg, ok := st.(activeDocCountStore); ok {
		counts, total, err := agg.ActiveDocCounts(ctx)
		if err == nil {
			return counts, total, nil
		}
		if !errors.Is(err, model.ErrNotImplemented) {
			return nil, 0, fmt.Errorf("active doc counts: %w", err)
		}
	}

	const pageSize = 500
	offset := 0
	counts := make(map[string]int64)
	var totalActive int64

	for {
		docs, total, err := st.ListFiles(ctx, "", "", pageSize, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("list files: %w", err)
		}
		for _, doc := range docs {
			if doc.Deleted {
				continue
			}
			docType := strings.TrimSpace(doc.DocType)
			if docType == "" {
				docType = "unknown"
			}
			counts[docType]++
			totalActive++
		}
		offset += len(docs)
		if len(docs) == 0 || int64(offset) >= total {
			break
		}
	}

	return counts, totalActive, nil
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
