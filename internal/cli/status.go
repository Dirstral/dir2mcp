package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Read state from disk and show progress",
	RunE:  runStatus,
}

func runStatus(_ *cobra.Command, _ []string) error {
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

	corpusPath := filepath.Join(stateDir, "corpus.json")
	data, err := os.ReadFile(corpusPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No index found at", stateDir, "- run 'dir2mcp up' first.")
			return nil
		}
		return err
	}

	var corpus struct {
		Root    string `json:"root"`
		Indexing struct {
			JobID         string `json:"job_id"`
			Running       bool   `json:"running"`
			Scanned       int    `json:"scanned"`
			Indexed       int    `json:"indexed"`
			Skipped       int    `json:"skipped"`
			Deleted       int    `json:"deleted"`
			Representations int  `json:"representations"`
			ChunksTotal   int    `json:"chunks_total"`
			EmbeddedOk    int    `json:"embedded_ok"`
			Errors        int    `json:"errors"`
		} `json:"indexing"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		return err
	}

	fmt.Println("Root:", corpus.Root)
	fmt.Println("State:", stateDir)
	fmt.Println("Indexing:")
	fmt.Println("  job_id:", corpus.Indexing.JobID)
	fmt.Println("  running:", corpus.Indexing.Running)
	fmt.Println("  scanned:", corpus.Indexing.Scanned)
	fmt.Println("  indexed:", corpus.Indexing.Indexed)
	fmt.Println("  skipped:", corpus.Indexing.Skipped)
	fmt.Println("  deleted:", corpus.Indexing.Deleted)
	fmt.Println("  representations:", corpus.Indexing.Representations)
	fmt.Println("  chunks_total:", corpus.Indexing.ChunksTotal)
	fmt.Println("  embedded_ok:", corpus.Indexing.EmbeddedOk)
	fmt.Println("  errors:", corpus.Indexing.Errors)
	return nil
}
