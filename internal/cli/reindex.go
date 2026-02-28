package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Force full rebuild of the index",
	RunE:  runReindex,
}

func runReindex(_ *cobra.Command, _ []string) error {
	// Stub: ingestion pipeline not implemented yet (Tia's task).
	// When ingest is ready, call ingest.NewIndexer(...).FullRebuild()
	return fmt.Errorf("reindex: not yet implemented (ingestion pipeline pending)")
}
