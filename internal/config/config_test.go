package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_UsesDotEnvWhenEnvIsMissing(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "MISTRAL_API_KEY=from_dotenv\nMISTRAL_BASE_URL=https://dotenv.local\n")

	withWorkingDir(t, tmp, func() {
		testEnv := map[string]string{}
		cfg, err := load("", testEnv)
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
		testEnv := map[string]string{
			"MISTRAL_API_KEY":  "from_env",
			"MISTRAL_BASE_URL": "https://env.local",
		}
		cfg, err := load("", testEnv)
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
		testEnv := map[string]string{}
		cfg, err := load("", testEnv)
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

// withWorkingDir changes the process working directory for the duration of fn.
// Because cwd is process-global, this helper is incompatible with t.Parallel()
// and must not be used in tests that call t.Parallel(). Tests that need
// concurrency should isolate execution (for example in a subprocess) or use
// explicit synchronization (for example a shared mutex).
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
