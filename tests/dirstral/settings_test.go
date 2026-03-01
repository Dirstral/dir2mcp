package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dir2mcp/internal/dirstral/config"
	"github.com/joho/godotenv"
)

type envState struct {
	value string
	ok    bool
}

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	return dir
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	before := make(map[string]envState, len(keys))
	for _, key := range keys {
		v, ok := os.LookupEnv(key)
		before[key] = envState{value: v, ok: ok}
		_ = os.Unsetenv(key)
	}
	t.Cleanup(func() {
		for _, key := range keys {
			st := before[key]
			if st.ok {
				_ = os.Setenv(key, st.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})
}

func findField(t *testing.T, fields []config.FieldInfo, key string) config.FieldInfo {
	t.Helper()
	for _, field := range fields {
		if field.Key == key {
			return field
		}
	}
	t.Fatalf("missing field %q", key)
	return config.FieldInfo{}
}

func TestConfigSaveAndLoadRoundTrip(t *testing.T) {
	chdirTemp(t)
	unsetEnv(t,
		"DIRSTRAL_MCP_URL",
		"DIRSTRAL_MCP_TRANSPORT",
		"DIRSTRAL_MODEL",
		"DIRSTRAL_VERBOSE",
		"DIRSTRAL_HOST_LISTEN",
		"DIRSTRAL_HOST_MCP_PATH",
		"ELEVENLABS_BASE_URL",
		"DIRSTRAL_VOICE",
	)

	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	cfg := config.Default()
	cfg.Model = "mistral-large-latest"
	cfg.Host.MCPPath = "/custom-mcp"

	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Model != cfg.Model {
		t.Fatalf("model mismatch: got %q want %q", loaded.Model, cfg.Model)
	}
	if loaded.Host.MCPPath != cfg.Host.MCPPath {
		t.Fatalf("mcp_path mismatch: got %q want %q", loaded.Host.MCPPath, cfg.Host.MCPPath)
	}
}

func TestSaveSecretWritesDotEnvLocalAndEnvironment(t *testing.T) {
	chdirTemp(t)
	unsetEnv(t, "ELEVENLABS_API_KEY")

	if err := config.SaveSecret("ELEVENLABS_API_KEY", "test-secret"); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	envLocal, err := godotenv.Read(".env.local")
	if err != nil {
		t.Fatalf("read .env.local: %v", err)
	}
	if got := envLocal["ELEVENLABS_API_KEY"]; got != "test-secret" {
		t.Fatalf(".env.local missing secret entry: got %q", got)
	}
	if got := os.Getenv("ELEVENLABS_API_KEY"); got != "test-secret" {
		t.Fatalf("env not updated: got %q", got)
	}
}

func TestDeleteSecretRemovesDotEnvLocalAndEnvironment(t *testing.T) {
	chdirTemp(t)
	unsetEnv(t, "ELEVENLABS_API_KEY")

	if err := config.SaveSecret("ELEVENLABS_API_KEY", "test-secret"); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	if err := config.DeleteSecret("ELEVENLABS_API_KEY"); err != nil {
		t.Fatalf("delete secret: %v", err)
	}

	b, err := os.ReadFile(".env.local")
	if err != nil {
		t.Fatalf("read .env.local: %v", err)
	}
	if strings.Contains(string(b), "ELEVENLABS_API_KEY=") {
		t.Fatalf("expected .env.local to remove ELEVENLABS_API_KEY, got %q", string(b))
	}
	if _, ok := os.LookupEnv("ELEVENLABS_API_KEY"); ok {
		t.Fatalf("expected ELEVENLABS_API_KEY to be unset")
	}
}

func TestEffectiveFieldsDetectConfigAndDefaultSources(t *testing.T) {
	chdirTemp(t)
	unsetEnv(t,
		"DIRSTRAL_MODEL",
		"DIRSTRAL_MCP_TRANSPORT",
		"DIR2MCP_AUTH_TOKEN",
		"ELEVENLABS_API_KEY",
	)

	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	cfg := config.Default()
	cfg.Model = "from-config-file"
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fields := config.EffectiveFields(loaded)
	model := findField(t, fields, "model")
	if model.Value != "from-config-file" {
		t.Fatalf("unexpected model value: got %q", model.Value)
	}
	if model.Source != config.SourceConfigFile {
		t.Fatalf("unexpected model source: got %q", model.Source)
	}

	transport := findField(t, fields, "mcp.transport")
	if transport.Source != config.SourceDefault {
		t.Fatalf("unexpected transport source: got %q", transport.Source)
	}
}

func TestEffectiveFieldsDetectDotEnvLocalForNormalAndSecretFields(t *testing.T) {
	chdirTemp(t)
	unsetEnv(t, "DIRSTRAL_MODEL", "DIR2MCP_AUTH_TOKEN", "ELEVENLABS_API_KEY")

	if err := os.WriteFile(".env.local", []byte("DIRSTRAL_MODEL=dotenv-model\nDIR2MCP_AUTH_TOKEN=token-from-local\n"), 0o644); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}
	if err := os.Setenv("DIRSTRAL_MODEL", "dotenv-model"); err != nil {
		t.Fatalf("set model env: %v", err)
	}
	if err := os.Setenv("DIR2MCP_AUTH_TOKEN", "token-from-local"); err != nil {
		t.Fatalf("set token env: %v", err)
	}

	fields := config.EffectiveFields(config.Default())
	model := findField(t, fields, "model")
	if model.Source != config.SourceDotEnvLocal {
		t.Fatalf("unexpected model source: got %q", model.Source)
	}

	secret := findField(t, fields, "DIR2MCP_AUTH_TOKEN")
	if secret.Source != config.SourceDotEnvLocal {
		t.Fatalf("unexpected token source: got %q", secret.Source)
	}
	if secret.Value != "token-from-local" {
		t.Fatalf("unexpected token value: got %q", secret.Value)
	}
}

func TestValidateApplyAndDefaultHelpers(t *testing.T) {
	if err := config.ValidateField("mcp.transport", "invalid"); err == nil {
		t.Fatalf("expected validation error for invalid transport")
	}

	cfg := config.Default()
	config.ApplyField(&cfg, "verbose", "true")
	if !cfg.Verbose {
		t.Fatalf("expected verbose=true after apply")
	}

	if got := config.DefaultValueForField("mcp.url"); got != config.Default().MCP.URL {
		t.Fatalf("unexpected default mcp.url: got %q", got)
	}
}

func TestDefaultConfigAlignsHostAndMCPDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.Host.Listen != config.DefaultHostListen {
		t.Fatalf("unexpected default host.listen: got %q want %q", cfg.Host.Listen, config.DefaultHostListen)
	}
	if cfg.Host.MCPPath != config.DefaultHostMCPPath {
		t.Fatalf("unexpected default host.mcp_path: got %q want %q", cfg.Host.MCPPath, config.DefaultHostMCPPath)
	}
	if cfg.MCP.URL != config.DefaultMCPURL {
		t.Fatalf("unexpected default mcp.url: got %q want %q", cfg.MCP.URL, config.DefaultMCPURL)
	}
	if cfg.MCP.Transport != config.DefaultMCPTransport {
		t.Fatalf("unexpected default mcp.transport: got %q want %q", cfg.MCP.Transport, config.DefaultMCPTransport)
	}
}
