package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SnapshotConfig returns a copy of config safe to persist: secrets replaced with
// source metadata only (SPEC: snapshot MUST NOT contain plaintext secrets).
func SnapshotConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	c := *cfg
	c.Mistral.APIKey = redactSecret(cfg.Mistral.APIKey, "MISTRAL_API_KEY")
	c.STT.Mistral.APIKey = redactSecret(cfg.STT.Mistral.APIKey, "MISTRAL_API_KEY")
	c.STT.ElevenLabs.APIKey = redactSecret(cfg.STT.ElevenLabs.APIKey, "ELEVENLABS_API_KEY")
	return &c
}

func redactSecret(value, envName string) string {
	if value == "" {
		return ""
	}
	return "<from env " + envName + ">"
}

// WriteSnapshot writes the redacted config snapshot to stateDir/.dir2mcp.yaml.snapshot.
func WriteSnapshot(stateDir string, cfg *Config) error {
	snap := SnapshotConfig(cfg)
	if snap == nil {
		return fmt.Errorf("config is nil")
	}
	data, err := yaml.Marshal(snap)
	if err != nil {
		return err
	}
	p := filepath.Join(stateDir, ".dir2mcp.yaml.snapshot")
	return os.WriteFile(p, data, 0600)
}
