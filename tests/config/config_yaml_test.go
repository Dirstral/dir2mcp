package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/tests/testutil"
)

func TestLoadFile_ReadsYAMLValues(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".dir2mcp.yaml")
	writeFile(t, path, ""+
		"root_dir: ./repo\n"+
		"state_dir: ./repo/.state\n"+
		"listen_addr: 127.0.0.1:7000\n"+
		"mcp_path: /custom\n"+
		"public: true\n"+
		"auth_mode: none\n"+
		"allowed_origins:\n"+
		"  - https://example.com\n")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if cfg.RootDir != "./repo" {
		t.Fatalf("RootDir=%q want=%q", cfg.RootDir, "./repo")
	}
	if cfg.StateDir != "./repo/.state" {
		t.Fatalf("StateDir=%q want=%q", cfg.StateDir, "./repo/.state")
	}
	if cfg.ListenAddr != "127.0.0.1:7000" {
		t.Fatalf("ListenAddr=%q want=%q", cfg.ListenAddr, "127.0.0.1:7000")
	}
	if cfg.MCPPath != "/custom" {
		t.Fatalf("MCPPath=%q want=%q", cfg.MCPPath, "/custom")
	}
	if !cfg.Public {
		t.Fatal("expected Public=true")
	}
	if cfg.AuthMode != "none" {
		t.Fatalf("AuthMode=%q want=%q", cfg.AuthMode, "none")
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "https://example.com" {
		t.Fatalf("AllowedOrigins=%v want=[https://example.com]", cfg.AllowedOrigins)
	}
}

func TestLoad_FileThenEnvOverridesYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".dir2mcp.yaml")
	writeFile(t, path, ""+
		"mistral_base_url: https://yaml.example\n"+
		"embed_model_text: yaml-embed\n")

	testutil.WithWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_BASE_URL", "https://env.example")
		t.Setenv("DIR2MCP_EMBED_MODEL_TEXT", "env-embed")

		cfg, err := config.Load(".dir2mcp.yaml")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.MistralBaseURL != "https://env.example" {
			t.Fatalf("MistralBaseURL=%q want=%q", cfg.MistralBaseURL, "https://env.example")
		}
		if cfg.EmbedModelText != "env-embed" {
			t.Fatalf("EmbedModelText=%q want=%q", cfg.EmbedModelText, "env-embed")
		}
	})
}

func TestSaveFile_WritesNonSecretYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".dir2mcp.yaml")

	cfg := config.Default()
	cfg.RootDir = "/tmp/repo"
	cfg.StateDir = "/tmp/repo/.dir2mcp"
	cfg.MistralAPIKey = "super-secret"
	cfg.ElevenLabsAPIKey = "another-secret"
	cfg.AllowedOrigins = []string{"http://localhost", "https://example.com"}

	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "root_dir: /tmp/repo") {
		t.Fatalf("saved yaml missing root_dir, got:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "mistral_api_key") {
		t.Fatalf("saved yaml must not include MISTRAL_API_KEY key, got:\n%s", text)
	}
	if strings.Contains(text, "super-secret") || strings.Contains(text, "another-secret") {
		t.Fatalf("saved yaml must not include secret values, got:\n%s", text)
	}
}

func TestLoadFile_MalformedYAMLReturnsError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".dir2mcp.yaml")
	writeFile(t, path, "root_dir: [unterminated\n")

	_, err := config.LoadFile(path)
	if err == nil {
		t.Fatal("expected LoadFile to fail on malformed YAML")
	}
	if !strings.Contains(err.Error(), "parse config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}
