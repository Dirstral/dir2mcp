package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultProtocolVersion = "2025-11-25"

type Config struct {
	RootDir         string
	StateDir        string
	ListenAddr      string
	MCPPath         string
	ProtocolVersion string
	Public          bool
	AuthMode        string
	AllowedOrigins  []string
}

func Default() Config {
	return Config{
		RootDir:         ".",
		StateDir:        filepath.Join(".", ".dir2mcp"),
		ListenAddr:      "127.0.0.1:0",
		MCPPath:         "/mcp",
		ProtocolVersion: DefaultProtocolVersion,
		Public:          false,
		AuthMode:        "auto",
		AllowedOrigins: []string{
			"http://localhost",
			"http://127.0.0.1",
		},
	}
}

func Load(path string) (Config, error) {
	// Skeleton loader: return defaults until config parsing is implemented.
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("stat config: %w", err)
	}

	return cfg, nil
}
