package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPrecedence_FlagsOverrideEnv verifies flags > env > file > defaults (issue #10).
func TestPrecedence_FlagsOverrideEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".dir2mcp.yaml")
	// File says listen 0.0.0.0:9999
	yamlContent := "version: 1\nserver:\n  listen: \"0.0.0.0:9999\"\n  mcp_path: \"/custom\"\n"
	if err := os.WriteFile(configPath, []byte(yamlContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Env: MISTRAL_API_KEY so validation passes
	os.Setenv("MISTRAL_API_KEY", "test-key")
	defer os.Unsetenv("MISTRAL_API_KEY")

	// Overrides (flags) should win over file
	listenOverride := "127.0.0.1:8888"
	mcpOverride := "/mcp"
	overrides := &Overrides{
		ServerListen:  &listenOverride,
		ServerMCPPath: &mcpOverride,
	}
	cfg, err := Load(Options{
		ConfigPath:   configPath,
		RootDir:      dir,
		SkipValidate: false,
		Overrides:    overrides,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "127.0.0.1:8888" {
		t.Errorf("expected Server.Listen from overrides 127.0.0.1:8888, got %q", cfg.Server.Listen)
	}
	if cfg.Server.MCPPath != "/mcp" {
		t.Errorf("expected Server.MCPPath from overrides /mcp, got %q", cfg.Server.MCPPath)
	}
}

// TestPrecedence_EnvOverridesFile verifies env overrides file when no CLI overrides.
func TestPrecedence_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".dir2mcp.yaml")
	// File has placeholder
	yamlContent := "version: 1\nmistral:\n  api_key: \"from-file\"\n"
	if err := os.WriteFile(configPath, []byte(yamlContent), 0600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("MISTRAL_API_KEY", "from-env")
	defer os.Unsetenv("MISTRAL_API_KEY")

	cfg, err := Load(Options{ConfigPath: configPath, RootDir: dir, SkipValidate: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mistral.APIKey != "from-env" {
		t.Errorf("expected api_key from env 'from-env', got %q", cfg.Mistral.APIKey)
	}
}

// TestSnapshot_NeverStoresPlaintextSecrets verifies snapshot redacts secrets (issue #10).
func TestSnapshot_NeverStoresPlaintextSecrets(t *testing.T) {
	cfg := Default()
	cfg.Mistral.APIKey = "sk-secret-mistral-key"
	cfg.STT.Mistral.APIKey = "sk-mistral-stt"
	cfg.STT.ElevenLabs.APIKey = "sk-elevenlabs-secret"

	snap := SnapshotConfig(cfg)
	if snap.Mistral.APIKey != "<from env MISTRAL_API_KEY>" {
		t.Errorf("Mistral.APIKey should be redacted, got %q", snap.Mistral.APIKey)
	}
	if snap.STT.Mistral.APIKey != "<from env MISTRAL_API_KEY>" {
		t.Errorf("STT.Mistral.APIKey should be redacted, got %q", snap.STT.Mistral.APIKey)
	}
	if snap.STT.ElevenLabs.APIKey != "<from env ELEVENLABS_API_KEY>" {
		t.Errorf("STT.ElevenLabs.APIKey should be redacted, got %q", snap.STT.ElevenLabs.APIKey)
	}
	// Ensure no raw secret appears in marshalled output
	data, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-secret") || strings.Contains(string(data), "sk-mistral") || strings.Contains(string(data), "sk-elevenlabs") {
		t.Errorf("snapshot must not contain plaintext secrets: %s", string(data))
	}
}
