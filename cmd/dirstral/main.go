package main

import (
	"fmt"
	"os"

	"dir2mcp/internal/dirstral/app"
)

func main() {
	if err := app.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
