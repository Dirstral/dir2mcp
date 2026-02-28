package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
			{Name: "text", Path: textIndexPath, Index: textIndex},
			{Name: "code", Path: codeIndexPath, Index: codeIndex},
		},
		15*time.Second,
		func(saveErr error) { fmt.Fprintf(os.Stderr, "index autosave error: %v\n", saveErr) },
	)
	if err := persistence.LoadAll(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "load indices: %v\n", err)
		return 5
	}
	persistence.Start(context.Background())
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
		OnIndexedChunk: func(label uint64, metadata model.SearchHit) {
			ret.SetChunkMetadata(label, metadata)
		},
	}
	codeWorker := &index.EmbeddingWorker{
		Source:       st,
		Index:        codeIndex,
		Embedder:     client,
		ModelForText: "mistral-embed",
		ModelForCode: "codestral-embed",
		BatchSize:    32,
		OnIndexedChunk: func(label uint64, metadata model.SearchHit) {
			ret.SetChunkMetadata(label, metadata)
		},
	}
	mcpServer := mcp.NewServer(cfg, ret)
	ing := ingest.NewService(cfg, st)

	if _, err := textWorker.RunOnce(context.Background(), "text"); err != nil {
		fmt.Fprintf(os.Stderr, "text embedding pass warning: %v\n", err)
	}
	if _, err := codeWorker.RunOnce(context.Background(), "code"); err != nil {
		fmt.Fprintf(os.Stderr, "code embedding pass warning: %v\n", err)
	}

	fmt.Printf("State dir: %s\n", cfg.StateDir)
	fmt.Printf("MCP endpoint (planned): http://%s%s\n", cfg.ListenAddr, cfg.MCPPath)
	fmt.Println("Skeleton wiring complete; server/indexing logic is not implemented yet.")

	_ = mcpServer
	_ = ing
	_ = textWorker
	_ = codeWorker
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
