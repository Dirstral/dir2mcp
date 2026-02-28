package tests

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"dir2mcp/internal/cli"
	"dir2mcp/internal/config"
)

func TestUpAllowedOriginsFlag_IsAcceptedByCLI(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MISTRAL_API_KEY", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewAppWithIO(&stdout, &stderr)

	withWorkingDir(t, tmp, func() {
		code := app.RunWithContext(context.Background(), []string{
			"--non-interactive",
			"up",
			"--allowed-origins",
			"https://elevenlabs.io",
		})
		if code != 2 {
			t.Fatalf("unexpected exit code: got=%d want=2 stderr=%s", code, stderr.String())
		}
	})

	errText := stderr.String()
	if strings.Contains(errText, "invalid up flags") {
		t.Fatalf("flag should be accepted by parser, got: %s", errText)
	}
	if !strings.Contains(errText, "MISTRAL_API_KEY") {
		t.Fatalf("expected config validation failure after parsing flags, got: %s", errText)
	}
}

func TestMergeAllowedOrigins_EnvAndCLIChainKeepsDefaults(t *testing.T) {
	merged := config.MergeAllowedOrigins(config.Default().AllowedOrigins, "https://elevenlabs.io")
	merged = config.MergeAllowedOrigins(merged, "https://my-app.example.com,HTTP://LOCALHOST")

	assertContains(t, merged, "http://localhost")
	assertContains(t, merged, "http://127.0.0.1")
	assertContains(t, merged, "https://elevenlabs.io")
	assertContains(t, merged, "https://my-app.example.com")

	localhostCount := 0
	for _, origin := range merged {
		if strings.EqualFold(origin, "http://localhost") {
			localhostCount++
		}
	}
	if localhostCount != 1 {
		t.Fatalf("expected localhost to be deduplicated, got %d (%v)", localhostCount, merged)
	}
}
