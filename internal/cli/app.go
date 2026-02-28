package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/Dirstral/dir2mcp/internal/index"
	"github.com/Dirstral/dir2mcp/internal/ingest"
	"github.com/Dirstral/dir2mcp/internal/mcp"
	"github.com/Dirstral/dir2mcp/internal/mistral"
	"github.com/Dirstral/dir2mcp/internal/model"
	"github.com/Dirstral/dir2mcp/internal/retrieval"
	"github.com/Dirstral/dir2mcp/internal/store"
)

type App struct{}

func NewApp() *App {
	return &App{}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.printUsage()
		return 0
	}

	switch args[0] {
	case "up":
		return a.runUp()
	case "status":
		return a.runStatus()
	case "ask":
		return a.runAsk(args[1:])
	case "reindex":
		return a.runReindex()
	case "config":
		return a.runConfig(args[1:])
	case "version":
		fmt.Println("dir2mcp skeleton v0.0.0-dev")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		a.printUsage()
		return 1
	}
}

func (a *App) printUsage() {
	fmt.Println("dir2mcp skeleton")
	fmt.Println("usage: dir2mcp <command>")
	fmt.Println("commands: up, status, ask, reindex, config, version")
}

func (a *App) runUp() int {
	cfg, err := config.Load(".dir2mcp.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 2
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create state dir: %v\n", err)
		return 1
	}

	// create a context that is canceled on SIGINT/SIGTERM so that
	// background goroutines receive a signal and can shut down
	// gracefully.  the returned `stop` function is also a cancel
	// and is deferred for cleanup once runUp returns.
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stateDB := filepath.Join(cfg.StateDir, "meta.sqlite")
	st := store.NewSQLiteStore(stateDB)
	textIndexPath := filepath.Join(cfg.StateDir, "vectors_text.hnsw")
	codeIndexPath := filepath.Join(cfg.StateDir, "vectors_code.hnsw")
	textIndex := index.NewHNSWIndex(textIndexPath)
	codeIndex := index.NewHNSWIndex(codeIndexPath)
	client := mistral.NewClient(cfg.MistralBaseURL, cfg.MistralAPIKey)
	ret := retrieval.NewService(st, textIndex, client, client)
	ret.SetCodeIndex(codeIndex)

	persistence := index.NewPersistenceManager(
		[]index.IndexedFile{
			{Path: textIndexPath, Index: textIndex},
			{Path: codeIndexPath, Index: codeIndex},
		},
		15*time.Second,
		func(saveErr error) { fmt.Fprintf(os.Stderr, "index autosave error: %v\n", saveErr) },
	)
	// load and start persistence using the same signal-aware context
	if err := persistence.LoadAll(runCtx); err != nil {
		fmt.Fprintf(os.Stderr, "load indices: %v\n", err)
		return 5
	}
	persistence.Start(runCtx)
	defer func() {
		if saveErr := persistence.StopAndSave(); saveErr != nil {
			fmt.Fprintf(os.Stderr, "final index save error: %v\n", saveErr)
		}
	}()

	textWorker := &index.EmbeddingWorker{
		Source:       st,
		Index:        textIndex,
		Embedder:     client,
		ModelForText: "mistral-embed",
		ModelForCode: "codestral-embed",
		BatchSize:    32,
		OnIndexedChunk: func(label int64, metadata model.ChunkMetadata) {
			ret.SetChunkMetadataForIndex("text", uint64(label), metadata.ToSearchHit())
		},
	}
	codeWorker := &index.EmbeddingWorker{
		Source:       st,
		Index:        codeIndex,
		Embedder:     client,
		ModelForText: "mistral-embed",
		ModelForCode: "codestral-embed",
		BatchSize:    32,
		OnIndexedChunk: func(label int64, metadata model.ChunkMetadata) {
			ret.SetChunkMetadataForIndex("code", uint64(label), metadata.ToSearchHit())
		},
	}
	mcpServer := mcp.NewServer(cfg, ret)
	ing := ingest.NewService(cfg, st)

	bootstrapMetadata(runCtx, st, ret)

	fmt.Printf("State dir: %s\n", cfg.StateDir)
	fmt.Printf("MCP endpoint (planned): http://%s%s\n", cfg.ListenAddr, cfg.MCPPath)
	fmt.Println("Skeleton wiring complete; server/indexing logic is not implemented yet.")

	go func() {
		if err := textWorker.Run(runCtx, 2*time.Second, "text"); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "text worker stopped: %v\n", err)
		}
	}()
	go func() {
		if err := codeWorker.Run(runCtx, 2*time.Second, "code"); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "code worker stopped: %v\n", err)
		}
	}()

	_ = ing
	if err := mcpServer.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "mcp server: %v\n", err)
		return 4
	}
	return 0
}

func (a *App) runStatus() int {
	fmt.Println("status command skeleton: not implemented")
	return 0
}

func (a *App) runAsk(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "ask command requires a question argument")
		return 1
	}
	fmt.Printf("ask command skeleton: %q\n", args[0])
	return 0
}

func (a *App) runReindex() int {
	st := store.NewSQLiteStore(filepath.Join(".dir2mcp", "meta.sqlite"))
	ing := ingest.NewService(config.Default(), st)
	err := ing.Reindex(context.Background())
	if errors.Is(err, model.ErrNotImplemented) {
		fmt.Println("reindex skeleton: ingestion pipeline not implemented yet")
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "reindex failed: %v\n", err)
		return 1
	}
	return 0
}

func (a *App) runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Println("config command skeleton: supported subcommands are init and print")
		return 0
	}
	switch args[0] {
	case "init":
		fmt.Println("config init skeleton: not implemented")
	case "print":
		cfg, err := config.Load(".dir2mcp.yaml")
		if err != nil {
			fmt.Fprintf(os.Stderr, "load config: %v\n", err)
			return 2
		}
		fmt.Printf(
			"root=%s state_dir=%s listen=%s mcp_path=%s mistral_base_url=%s mistral_api_key_set=%t\n",
			cfg.RootDir,
			cfg.StateDir,
			cfg.ListenAddr,
			cfg.MCPPath,
			cfg.MistralBaseURL,
			cfg.MistralAPIKey != "",
		)
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", args[0])
		return 1
	}
	return 0
}

type embeddedMetadataStore interface {
	ListEmbeddedChunkMetadata(ctx context.Context, indexKind string, limit, offset int) ([]model.ChunkTask, error)
}

func bootstrapMetadata(ctx context.Context, st embeddedMetadataStore, ret *retrieval.Service) {
	if st == nil || ret == nil {
		return
	}
	const pageSize = 500
	for _, kind := range []string{"text", "code"} {
		offset := 0
		for {
			tasks, err := st.ListEmbeddedChunkMetadata(ctx, kind, pageSize, offset)
			if err != nil {
				fmt.Fprintf(os.Stderr, "metadata bootstrap warning (%s): %v\n", kind, err)
				break
			}
			if len(tasks) == 0 {
				break
			}
			for _, task := range tasks {
				ret.SetChunkMetadataForIndex(kind, uint64(task.Label), model.SearchHit{
					ChunkID: task.Metadata.ChunkID,
					RelPath: task.Metadata.RelPath,
					DocType: task.Metadata.DocType,
					RepType: task.Metadata.RepType,
					Snippet: task.Metadata.Snippet,
					Span:    task.Metadata.Span,
				})
			}
			offset += len(tasks)
		}
	}
}
