package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	stateDir := ".dir2mcp"
	cfg, err := config.Load(config.Options{
		ConfigPath: ".dir2mcp.yaml",
		RootDir:    ".",
		StateDir:   stateDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 2
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create state dir: %v\n", err)
		return 1
	}

	stateDB := filepath.Join(stateDir, "meta.sqlite")
	st := store.NewSQLiteStore(stateDB)
	ix := index.NewHNSWIndex(filepath.Join(stateDir, "vectors_text.hnsw"))
	client := mistral.NewClient("", cfg.Mistral.APIKey)
	ret := retrieval.NewService(st, ix, client)
	mcpServer, err := mcp.NewServer(mcp.ServerOptions{
		RootDir:   ".",
		StateDir:  stateDir,
		Config:    cfg,
		McpPath:   cfg.Server.MCPPath,
		AuthToken: "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp server: %v\n", err)
		return 1
	}
	ing := ingest.NewService(*cfg, st)

	fmt.Printf("State dir: %s\n", stateDir)
	fmt.Printf("MCP endpoint (planned): http://%s%s\n", cfg.Server.Listen, cfg.Server.MCPPath)
	fmt.Println("Skeleton wiring complete; server/indexing logic is not implemented yet.")

	_ = mcpServer
	_ = ing
	_ = ret
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
		cfg, err := config.Load(config.Options{
			ConfigPath:   ".dir2mcp.yaml",
			RootDir:      ".",
			StateDir:     ".dir2mcp",
			SkipValidate: true,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "load config: %v\n", err)
			return 2
		}
		fmt.Printf(
			"root=. state_dir=.dir2mcp listen=%s mcp_path=%s mistral_api_key_set=%t\n",
			cfg.Server.Listen,
			cfg.Server.MCPPath,
			cfg.Mistral.APIKey != "",
		)
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", args[0])
		return 1
	}
	return 0
}
