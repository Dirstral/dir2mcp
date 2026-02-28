package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "0.4.0"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	RunE:  runVersion,
}

func runVersion(_ *cobra.Command, _ []string) error {
	fmt.Println("dir2mcp", version)
	return nil
}
