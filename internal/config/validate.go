package config

import (
	"fmt"
	"strings"
)

// Validate checks required fields and enum constraints. For non-interactive mode,
// returns an error with actionable message (e.g. "Set env: MISTRAL_API_KEY=...") so caller can exit 2.
func Validate(cfg *Config, nonInteractive bool) error {
	if cfg == nil {
		return fmt.Errorf("CONFIG_INVALID: nil config")
	}
	// Embeddings require Mistral API key (or later: local embed connector)
	if cfg.Mistral.APIKey == "" || cfg.Mistral.APIKey == "${MISTRAL_API_KEY}" {
		return fmt.Errorf("CONFIG_INVALID: Missing MISTRAL_API_KEY\nSet env: MISTRAL_API_KEY=...\nOr run: dir2mcp config init")
	}
	// Enum validation: fail fast on invalid YAML/env values
	if err := validateEnums(cfg); err != nil {
		return err
	}
	return nil
}

// validateEnums checks constrained string fields against allowed values.
func validateEnums(cfg *Config) error {
	if !stringIn(cfg.Ingest.PDF.Mode, IngestModePDF) {
		return fmt.Errorf("CONFIG_INVALID: ingest.pdf.mode=%q; allowed: %s", cfg.Ingest.PDF.Mode, strings.Join(IngestModePDF, ", "))
	}
	if !stringIn(cfg.Ingest.Images.Mode, IngestModeImages) {
		return fmt.Errorf("CONFIG_INVALID: ingest.images.mode=%q; allowed: %s", cfg.Ingest.Images.Mode, strings.Join(IngestModeImages, ", "))
	}
	if !stringIn(cfg.Ingest.Audio.Mode, IngestModeAudio) {
		return fmt.Errorf("CONFIG_INVALID: ingest.audio.mode=%q; allowed: %s", cfg.Ingest.Audio.Mode, strings.Join(IngestModeAudio, ", "))
	}
	if !stringIn(cfg.Ingest.Archives.Mode, IngestModeArchives) {
		return fmt.Errorf("CONFIG_INVALID: ingest.archives.mode=%q; allowed: %s", cfg.Ingest.Archives.Mode, strings.Join(IngestModeArchives, ", "))
	}
	if !stringIn(cfg.STT.Provider, STTProviders) {
		return fmt.Errorf("CONFIG_INVALID: stt.provider=%q; allowed: %s", cfg.STT.Provider, strings.Join(STTProviders, ", "))
	}
	if !stringIn(cfg.X402.Mode, X402Modes) {
		return fmt.Errorf("CONFIG_INVALID: x402.mode=%q; allowed: %s", cfg.X402.Mode, strings.Join(X402Modes, ", "))
	}
	if !stringIn(cfg.Secrets.Provider, SecretsProviders) {
		return fmt.Errorf("CONFIG_INVALID: secrets.provider=%q; allowed: %s", cfg.Secrets.Provider, strings.Join(SecretsProviders, ", "))
	}
	if !stringIn(cfg.Security.Auth.Mode, SecurityAuthModes) {
		return fmt.Errorf("CONFIG_INVALID: security.auth.mode=%q; allowed: %s", cfg.Security.Auth.Mode, strings.Join(SecurityAuthModes, ", "))
	}
	return nil
}
