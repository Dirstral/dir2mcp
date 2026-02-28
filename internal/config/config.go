package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	MistralAPIKey   string
	MistralBaseURL  string
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
		MistralAPIKey:   "",
		MistralBaseURL:  "",
	}
}

func Load(path string) (Config, error) {
	// Skeleton loader: return defaults until config parsing is implemented.
	cfg := Default()
	loadDotEnvFiles(".env.local", ".env")
	if path == "" {
		applyEnvOverrides(&cfg)
		return cfg, nil
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			applyEnvOverrides(&cfg)
			return cfg, nil
		}
		return Config{}, fmt.Errorf("stat config: %w", err)
	}

	applyEnvOverrides(&cfg)
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if apiKey := strings.TrimSpace(os.Getenv("MISTRAL_API_KEY")); apiKey != "" {
		cfg.MistralAPIKey = apiKey
	}
	if baseURL := strings.TrimSpace(os.Getenv("MISTRAL_BASE_URL")); baseURL != "" {
		cfg.MistralBaseURL = baseURL
	}
}

func loadDotEnvFiles(paths ...string) {
	for _, p := range paths {
		_ = loadDotEnvFile(p)
	}
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if existing, exists := os.LookupEnv(key); exists && strings.TrimSpace(existing) != "" {
			continue
		}
		if setErr := os.Setenv(key, unquoteEnvValue(value)); setErr != nil {
			continue
		}
	}

	return scanner.Err()
}

func unquoteEnvValue(v string) string {
	if len(v) >= 2 {
		if (strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"")) ||
			(strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) {
			return v[1 : len(v)-1]
		}
	}
	return v
}
