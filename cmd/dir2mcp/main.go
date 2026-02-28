package main

import (
	"os"

	"github.com/Dirstral/dir2mcp/internal/cli"
)

func main() {
	app := cli.NewApp()
	os.Exit(app.Run(os.Args[1:]))
}
