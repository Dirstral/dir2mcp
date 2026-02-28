package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Options for loading config. ConfigPath is relative to RootDir if not absolute.
type Options struct {
	ConfigPath     string
	RootDir        string
	StateDir       string
	NonInteractive bool
	JSON           bool
	SkipValidate   bool // if true, do not validate (e.g. for config print)
	// Overrides apply last (flags > env > file > defaults). Nil means no CLI overrides.
	Overrides *Overrides
}

// Overrides holds CLI flag values that take precedence over env/file/defaults (issue #10).
// Only non-nil fields are applied.
type Overrides struct {
	ServerListen  *string
	ServerMCPPath *string
	ServerPublic  *bool
	MistralAPIKey *string
}

// Load builds config with precedence: defaults → .dir2mcp.yaml → env vars → Overrides (issue #10).
// Returns error suitable for exit code 2 when invalid.
func Load(opts Options) (*Config, error) {
	cfg := Default()

	// Load optional local dotenv files for developer ergonomics.
	// Precedence stays: explicit env > .env.local > .env.
	if err := loadDotEnvFiles(".env.local", ".env"); err != nil {
		return nil, fmt.Errorf("CONFIG_INVALID: failed loading dotenv files: %w", err)
	}

	configPath := opts.ConfigPath
	if !filepath.IsAbs(configPath) && opts.RootDir != "" {
		configPath = filepath.Join(opts.RootDir, configPath)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("CONFIG_INVALID: cannot read config file %s: %w", configPath, err)
		}
	} else {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("CONFIG_INVALID: malformed YAML in %s: %w", configPath, err)
		}
	}

	// Env overlay (SPEC §16.1.1)
	if v := os.Getenv("MISTRAL_API_KEY"); v != "" {
		cfg.Mistral.APIKey = v
		cfg.STT.Mistral.APIKey = v
	}
	if v := os.Getenv("ELEVENLABS_API_KEY"); v != "" {
		cfg.STT.ElevenLabs.APIKey = v
	}
	if v := os.Getenv("DIR2MCP_AUTH_TOKEN"); v != "" {
		cfg.Security.Auth.TokenEnv = "DIR2MCP_AUTH_TOKEN"
	}

	// CLI overrides (highest precedence)
	if opts.Overrides != nil {
		applyOverrides(&cfg, opts.Overrides)
	}

	if !opts.SkipValidate {
		if err := Validate(&cfg, opts.NonInteractive); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func applyOverrides(cfg *Config, o *Overrides) {
	if o.ServerListen != nil {
		cfg.Server.Listen = *o.ServerListen
	}
	if o.ServerMCPPath != nil {
		cfg.Server.MCPPath = *o.ServerMCPPath
	}
	if o.ServerPublic != nil {
		cfg.Server.Public = *o.ServerPublic
	}
	if o.MistralAPIKey != nil {
		cfg.Mistral.APIKey = *o.MistralAPIKey
	}
}
