package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_UsesDotEnvWhenEnvMissing(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_dotenv\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_API_KEY", "")
		cfg, err := Load(Options{
			ConfigPath:   ".dir2mcp.yaml",
			RootDir:      ".",
			StateDir:     ".dir2mcp",
			SkipValidate: true,
		})
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.Mistral.APIKey != "from_dotenv" {
			t.Fatalf("unexpected api key: %q", cfg.Mistral.APIKey)
		}
	})
}

func TestLoad_EnvOverridesDotEnv(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_dotenv\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_API_KEY", "from_env")
		cfg, err := Load(Options{
			ConfigPath:   ".dir2mcp.yaml",
			RootDir:      ".",
			StateDir:     ".dir2mcp",
			SkipValidate: true,
		})
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.Mistral.APIKey != "from_env" {
			t.Fatalf("unexpected api key: %q", cfg.Mistral.APIKey)
		}
	})
}

func TestLoad_DotEnvLocalOverridesDotEnv(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_env_file\n")
	writeFile(t, filepath.Join(tmp, ".env.local"), "MISTRAL_API_KEY=from_env_local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("MISTRAL_API_KEY", "")
		cfg, err := Load(Options{
			ConfigPath:   ".dir2mcp.yaml",
			RootDir:      ".",
			StateDir:     ".dir2mcp",
			SkipValidate: true,
		})
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.Mistral.APIKey != "from_env_local" {
			t.Fatalf("unexpected api key: %q", cfg.Mistral.APIKey)
		}
	})
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(original); chdirErr != nil {
			t.Fatalf("restore Chdir failed: %v", chdirErr)
		}
	}()
	fn()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
