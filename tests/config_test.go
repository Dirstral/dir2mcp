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

func TestLoad_UsesDotEnvWhenEnvIsMissing_ElevenLabs(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "ELEVENLABS_API_KEY=el_from_dotenv\nELEVENLABS_BASE_URL=https://el-dotenv.local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("ELEVENLABS_API_KEY", "")
		t.Setenv("ELEVENLABS_BASE_URL", "")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.ElevenLabsAPIKey != "el_from_dotenv" {
			t.Fatalf("unexpected elevenlabs api key: %q", cfg.ElevenLabsAPIKey)
		}
		if cfg.ElevenLabsBaseURL != "https://el-dotenv.local" {
			t.Fatalf("unexpected elevenlabs base URL: %q", cfg.ElevenLabsBaseURL)
		}
	})
}

func TestLoad_EnvOverridesDotEnv_ElevenLabs(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "ELEVENLABS_API_KEY=el_from_dotenv\nELEVENLABS_BASE_URL=https://el-dotenv.local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("ELEVENLABS_API_KEY", "el_from_env")
		t.Setenv("ELEVENLABS_BASE_URL", "https://el-env.local")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.ElevenLabsAPIKey != "el_from_env" {
			t.Fatalf("unexpected elevenlabs api key: %q", cfg.ElevenLabsAPIKey)
		}
		if cfg.ElevenLabsBaseURL != "https://el-env.local" {
			t.Fatalf("unexpected elevenlabs base URL: %q", cfg.ElevenLabsBaseURL)
		}
	})
}

func TestLoad_DotEnvLocalOverridesDotEnv_ElevenLabs(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, ".env"), "ELEVENLABS_API_KEY=el_env_file\nELEVENLABS_BASE_URL=https://el-env-file.local\n")
	writeFile(t, filepath.Join(tmp, ".env.local"), "ELEVENLABS_API_KEY=el_env_local\nELEVENLABS_BASE_URL=https://el-env-local.local\n")

	withWorkingDir(t, tmp, func() {
		t.Setenv("ELEVENLABS_API_KEY", "")
		t.Setenv("ELEVENLABS_BASE_URL", "")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.ElevenLabsAPIKey != "el_env_local" {
			t.Fatalf("unexpected elevenlabs api key: %q", cfg.ElevenLabsAPIKey)
		}
		if cfg.ElevenLabsBaseURL != "https://el-env-local.local" {
			t.Fatalf("unexpected elevenlabs base URL: %q", cfg.ElevenLabsBaseURL)
		}
	})
}

func TestLoad_AllowedOriginsEnvAppendsToDefaults(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "https://elevenlabs.io,https://my-app.example.com")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		assertContains(t, cfg.AllowedOrigins, "http://localhost")
		assertContains(t, cfg.AllowedOrigins, "http://127.0.0.1")
		assertContains(t, cfg.AllowedOrigins, "https://elevenlabs.io")
		assertContains(t, cfg.AllowedOrigins, "https://my-app.example.com")
	})
}

func TestLoad_AllowedOriginsEnvDeduplicatesHostCase(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "HTTP://LOCALHOST,https://elevenlabs.io")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		localhostCount := 0
		for _, origin := range cfg.AllowedOrigins {
			if origin == "http://localhost" || origin == "HTTP://LOCALHOST" {
				localhostCount++
			}
		}
		if localhostCount != 1 {
			t.Fatalf("expected exactly one localhost origin entry, got %d (%v)", localhostCount, cfg.AllowedOrigins)
		}
		assertContains(t, cfg.AllowedOrigins, "https://elevenlabs.io")
	})
}

func TestLoad_AllowedOriginsEnvSkipsMalformedOrigins(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "://bad-origin,https://elevenlabs.io")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		assertNotContains(t, cfg.AllowedOrigins, "://bad-origin")
		assertContains(t, cfg.AllowedOrigins, "https://elevenlabs.io")
		assertContains(t, cfg.AllowedOrigins, "http://localhost")
	})
}

func TestLoad_AllowedOriginsEnvSkipsPathLikeToken(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "bad/path,https://elevenlabs.io")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		assertNotContains(t, cfg.AllowedOrigins, "bad/path")
		assertContains(t, cfg.AllowedOrigins, "https://elevenlabs.io")
	})
}

func TestLoad_AllowedOriginsEnvSkipsBackslashToken(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "bad\\path,https://elevenlabs.io")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		assertNotContains(t, cfg.AllowedOrigins, "bad\\path")
		assertContains(t, cfg.AllowedOrigins, "https://elevenlabs.io")
	})
}

func TestLoad_AllowedOriginsEnvSkipsWhitespaceToken(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "bad origin,https://elevenlabs.io")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		assertNotContains(t, cfg.AllowedOrigins, "bad origin")
		assertContains(t, cfg.AllowedOrigins, "https://elevenlabs.io")
	})
}

func TestLoad_AllowedOriginsEnvDeduplicatesHTTPSDefaultPort(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "https://example.com,https://example.com:443")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		count := 0
		for _, origin := range cfg.AllowedOrigins {
			if origin == "https://example.com" || origin == "https://example.com:443" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected one normalized https example.com entry, got %d (%v)", count, cfg.AllowedOrigins)
		}
		assertContains(t, cfg.AllowedOrigins, "https://example.com")
	})
}

func TestLoad_AllowedOriginsEnvDeduplicatesHTTPDefaultPort(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "http://example.com,http://example.com:80")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		count := 0
		for _, origin := range cfg.AllowedOrigins {
			if origin == "http://example.com" || origin == "http://example.com:80" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected one normalized http example.com entry, got %d (%v)", count, cfg.AllowedOrigins)
		}
		assertContains(t, cfg.AllowedOrigins, "http://example.com")
	})
}

func TestLoad_AllowedOriginsEnvKeepsNonDefaultPortDistinct(t *testing.T) {
	tmp := t.TempDir()

	withWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_ALLOWED_ORIGINS", "https://example.com,https://example.com:444")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		assertContains(t, cfg.AllowedOrigins, "https://example.com")
		assertContains(t, cfg.AllowedOrigins, "https://example.com:444")
	})
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, values)
}

func assertNotContains(t *testing.T, values []string, needle string) {
	t.Helper()
	for _, value := range values {
		if value == needle {
			t.Fatalf("did not expect %q in %v", needle, values)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
