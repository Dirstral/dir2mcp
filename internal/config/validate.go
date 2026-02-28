package config

import (
	"fmt"
	"strings"
)

// Validate checks required fields. For non-interactive mode, returns an error
// with actionable message (e.g. "Set env: MISTRAL_API_KEY=...") so caller can exit 2.
func Validate(cfg *Config, nonInteractive bool) error {
	// Embeddings require Mistral API key (or later: local embed connector)
	if cfg.Mistral.APIKey == "" || cfg.Mistral.APIKey == "${MISTRAL_API_KEY}" {
		if nonInteractive {
			return fmt.Errorf("CONFIG_INVALID: Missing MISTRAL_API_KEY\nSet env: MISTRAL_API_KEY=...\nOr run: dir2mcp config init")
		}
		// Interactive: we could prompt later; for now still return so caller can decide
		return fmt.Errorf("CONFIG_INVALID: Missing MISTRAL_API_KEY\nSet env: MISTRAL_API_KEY=...\nOr run: dir2mcp config init")
	}
	if cfg.Server.MCPPath == "" || !strings.HasPrefix(cfg.Server.MCPPath, "/") {
		return fmt.Errorf("CONFIG_INVALID: server.mcp_path must start with '/'")
	}
	return nil
}
