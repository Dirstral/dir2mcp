package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Dirstral/dir2mcp/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create or update .dir2mcp.yaml with defaults",
	RunE:  runConfigInit,
}

var configPrintCmd = &cobra.Command{
	Use:   "print",
	Short: "Print effective config as YAML (secrets redacted)",
	RunE:  runConfigPrint,
}

func init() {
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configPrintCmd)
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	rootDir, err := filepath.Abs(globalFlags.Dir)
	if err != nil {
		return err
	}
	configPath := globalFlags.ConfigPath
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(rootDir, configPath)
	}

	// Write default config (issue #10: config init)
	if err := os.WriteFile(configPath, []byte(config.DefaultYAML), 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	fmt.Println("Wrote", configPath)

	// Interactive wizard: TTY-aware, masked secrets (issue #10)
	if !globalFlags.NonInteractive && IsTTY() {
		// Prompt for Mistral API key (masked). We do not persist it; user must set env.
		fmt.Fprintln(os.Stderr, "Optional: enter your Mistral API key now (input is hidden). Press Enter to skip and set MISTRAL_API_KEY later.")
		key, err := ReadSecret("Mistral API key: ")
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if key != "" {
			fmt.Fprintln(os.Stderr, "Key received. Set it in your environment before running 'dir2mcp up':")
			fmt.Fprintln(os.Stderr, "  export MISTRAL_API_KEY=<your-key>")
		}
	} else {
		fmt.Println("Edit the file or set MISTRAL_API_KEY (and optionally ELEVENLABS_API_KEY) in your environment.")
	}
	return nil
}

func runConfigPrint(_ *cobra.Command, _ []string) error {
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
		ConfigPath:     globalFlags.ConfigPath,
		RootDir:        rootDir,
		StateDir:       stateDir,
		NonInteractive: true,
		JSON:           globalFlags.JSON,
		SkipValidate:   true, // print even when API key not set
	})
	if err != nil {
		exitWith(ExitConfigInvalid, "ERROR: "+err.Error())
	}

	snap := config.SnapshotConfig(cfg)
	data, err := yaml.Marshal(snap)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}
