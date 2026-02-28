package tests

import (
	"os"
	"path/filepath"
	"testing"

	"dir2mcp/internal/config"
)

func TestLoad_UsesDotEnvWhenEnvIsMissing(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_dotenv\nMISTRAL_BASE_URL=https://dotenv.local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_API_KEY", "")
		t.Setenv("MISTRAL_BASE_URL", "")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.MistralAPIKey != "from_dotenv" {
			t.Fatalf("unexpected api key: %q", cfg.MistralAPIKey)
		}
		if cfg.MistralBaseURL != "https://dotenv.local" {
			t.Fatalf("unexpected base URL: %q", cfg.MistralBaseURL)
		}
	})
}

func TestLoad_EnvOverridesDotEnv(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_dotenv\nMISTRAL_BASE_URL=https://dotenv.local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_API_KEY", "from_env")
		t.Setenv("MISTRAL_BASE_URL", "https://env.local")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.MistralAPIKey != "from_env" {
			t.Fatalf("unexpected api key: %q", cfg.MistralAPIKey)
		}
		if cfg.MistralBaseURL != "https://env.local" {
			t.Fatalf("unexpected base URL: %q", cfg.MistralBaseURL)
		}
	})
}

func TestLoad_DotEnvLocalOverridesDotEnv(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_env_file\nMISTRAL_BASE_URL=https://env-file.local\n")
	writeFile(t, filepath.Join(tmp, ".env.local"), "MISTRAL_API_KEY=from_env_local\nMISTRAL_BASE_URL=https://env-local.local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_API_KEY", "")
		t.Setenv("MISTRAL_BASE_URL", "")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.MistralAPIKey != "from_env_local" {
			t.Fatalf("unexpected api key: %q", cfg.MistralAPIKey)
		}
		if cfg.MistralBaseURL != "https://env-local.local" {
			t.Fatalf("unexpected base URL: %q", cfg.MistralBaseURL)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
