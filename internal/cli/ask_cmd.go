package cli

import (
	"fmt"
	"path/filepath"

	"dir2mcp/internal/config"
	"dir2mcp/internal/retrieval"
	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Local convenience: run RAG via the same engine (no MCP)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAsk,
}

func runAsk(_ *cobra.Command, args []string) error {
	rootDir, err := filepath.Abs(globalFlags.Dir)
	if err != nil {
		return err
	}
	stateDir := globalFlags.StateDir
	if stateDir == "" {
		stateDir = filepath.Join(rootDir, ".dir2mcp")
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		return err
	}

	cfg, err := config.Load(config.Options{
		ConfigPath:      globalFlags.ConfigPath,
		RootDir:         rootDir,
		StateDir:        stateDir,
		NonInteractive:  globalFlags.NonInteractive,
		JSON:            globalFlags.JSON,
	})
	if err != nil {
		exitWith(ExitConfigInvalid, "ERROR: "+err.Error())
	}

	engine, err := retrieval.NewEngine(stateDir, rootDir, cfg)
	if err != nil {
		return fmt.Errorf("retrieval engine: %w", err)
	}
	defer engine.Close()

	question := args[0]
	k := cfg.RAG.KDefault
	if k <= 0 {
		k = 10
	}
	result, err := engine.Ask(question, retrieval.AskOptions{K: k})
	if err != nil {
		return err
	}
	fmt.Println(result.Answer)
	if len(result.Citations) > 0 {
		fmt.Println()
		fmt.Println("Citations:")
		for _, c := range result.Citations {
			fmt.Printf("  %s %v\n", c.RelPath, c.Span)
		}
	}
	return nil
}
