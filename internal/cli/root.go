package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes per SPEC §2.4
const (
	ExitSuccess           = 0
	ExitGenericError     = 1
	ExitConfigInvalid    = 2
	ExitRootInaccessible = 3
	ExitBindFailure      = 4
	ExitIndexLoadFailure = 5
	ExitIngestionFatal   = 6
)

// GlobalFlags holds flags shared across all commands (dir, config, state-dir, json, etc.).
type GlobalFlags struct {
	Dir             string
	ConfigPath      string
	StateDir        string
	JSON           bool
	NonInteractive bool
	Quiet          bool
}

var globalFlags GlobalFlags

var rootCmd = &cobra.Command{
	Use:   "dir2mcp",
	Short: "Deploy-first MCP server for private directory data",
	Long:  "dir2mcp turns a directory of privately hosted data into a standard MCP tool server in one command.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalFlags.Dir, "dir", ".", "root directory to index")
	rootCmd.PersistentFlags().StringVar(&globalFlags.ConfigPath, "config", ".dir2mcp.yaml", "config file path")
	rootCmd.PersistentFlags().StringVar(&globalFlags.StateDir, "state-dir", "", "state directory (default: <root>/.dir2mcp)")
	rootCmd.PersistentFlags().BoolVar(&globalFlags.JSON, "json", false, "emit NDJSON events for automation/logging")
	rootCmd.PersistentFlags().BoolVar(&globalFlags.NonInteractive, "non-interactive", false, "disable prompts; fail fast with actionable instructions when config missing")
	rootCmd.PersistentFlags().BoolVar(&globalFlags.Quiet, "quiet", false, "reduce output")

	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(reindexCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
}

// Execute runs the root command and returns an error; exit code is set by RunE.
func Execute() error {
	return rootCmd.Execute()
}

// exitWith prints message to stderr and exits with code.
func exitWith(code int, msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(code)
}
