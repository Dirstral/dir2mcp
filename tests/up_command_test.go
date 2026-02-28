package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"dir2mcp/internal/cli"
	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

var cwdMu sync.Mutex

func TestUpCreatesSecretTokenAndConnectionFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("DIR2MCP_AUTH_TOKEN", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewAppWithIO(&stdout, &stderr)

	withWorkingDir(t, tmp, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		code := app.RunWithContext(ctx, []string{"up", "--listen", "127.0.0.1:0"})
		if code != 0 {
			t.Fatalf("unexpected exit code: got=%d stderr=%s", code, stderr.String())
		}
	})

	secretTokenPath := filepath.Join(tmp, ".dir2mcp", "secret.token")
	tokenRaw, err := os.ReadFile(secretTokenPath)
	if err != nil {
		t.Fatalf("read secret token: %v", err)
	}
	token := strings.TrimSpace(string(tokenRaw))
	if len(token) != 64 {
		t.Fatalf("unexpected token length: got=%d want=64", len(token))
	}

	tokenInfo, err := os.Stat(secretTokenPath)
	if err != nil {
		t.Fatalf("stat secret token: %v", err)
	}
	if runtime.GOOS != "windows" && tokenInfo.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected token permissions: got=%#o want=%#o", tokenInfo.Mode().Perm(), 0o600)
	}

	connectionPath := filepath.Join(tmp, ".dir2mcp", "connection.json")
	connectionRaw, err := os.ReadFile(connectionPath)
	if err != nil {
		t.Fatalf("read connection.json: %v", err)
	}

	var connection struct {
		Transport string            `json:"transport"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		Session   struct {
			UsesMCPSessionID     bool   `json:"uses_mcp_session_id"`
			HeaderName           string `json:"header_name"`
			AssignedOnInitialize bool   `json:"assigned_on_initialize"`
		} `json:"session"`
		TokenSource string `json:"token_source"`
		TokenFile   string `json:"token_file"`
	}
	if err := json.Unmarshal(connectionRaw, &connection); err != nil {
		t.Fatalf("unmarshal connection.json: %v", err)
	}

	if connection.Transport != "mcp_streamable_http" {
		t.Fatalf("unexpected transport: %q", connection.Transport)
	}
	if !strings.HasSuffix(connection.URL, "/mcp") {
		t.Fatalf("unexpected connection URL: %q", connection.URL)
	}
	if connection.Headers["MCP-Protocol-Version"] != config.DefaultProtocolVersion {
		t.Fatalf("unexpected protocol version header: %q", connection.Headers["MCP-Protocol-Version"])
	}
	if connection.TokenSource != "secret.token" {
		t.Fatalf("unexpected token_source: %q", connection.TokenSource)
	}
	if connection.TokenFile == "" {
		t.Fatal("expected token_file to be populated")
	}
	if !connection.Session.UsesMCPSessionID {
		t.Fatal("expected session.uses_mcp_session_id=true")
	}
	if connection.Session.HeaderName != "MCP-Session-Id" {
		t.Fatalf("unexpected session.header_name: %q", connection.Session.HeaderName)
	}
	if !connection.Session.AssignedOnInitialize {
		t.Fatal("expected session.assigned_on_initialize=true")
	}
}

func TestUpNonInteractiveMissingConfigReturnsExitCode2(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MISTRAL_API_KEY", "")
	t.Setenv("DIR2MCP_AUTH_TOKEN", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewAppWithIO(&stdout, &stderr)

	var code int
	withWorkingDir(t, tmp, func() {
		code = app.RunWithContext(context.Background(), []string{"--non-interactive", "up"})
	})

	if code != 2 {
		t.Fatalf("unexpected exit code: got=%d want=2", code)
	}

	errText := stderr.String()
	if !strings.Contains(errText, "CONFIG_INVALID") {
		t.Fatalf("expected CONFIG_INVALID in stderr, got: %s", errText)
	}
	if !strings.Contains(errText, "MISTRAL_API_KEY") {
		t.Fatalf("expected MISTRAL_API_KEY hint in stderr, got: %s", errText)
	}
	if !strings.Contains(errText, "dir2mcp config init") {
		t.Fatalf("expected config init hint in stderr, got: %s", errText)
	}
}

func TestUpJSONConnectionEventIncludesTokenSourceForFileAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("DIR2MCP_AUTH_TOKEN", "")

	customTokenPath := filepath.Join(tmp, "external.token")
	if err := os.WriteFile(customTokenPath, []byte("external-token-value\n"), 0o600); err != nil {
		t.Fatalf("write custom token: %v", err)
	}
	customTokenAbs := customTokenPath
	if absPath, err := filepath.Abs(customTokenPath); err == nil {
		customTokenAbs = absPath
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewAppWithIO(&stdout, &stderr)

	withWorkingDir(t, tmp, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		code := app.RunWithContext(ctx, []string{
			"up",
			"--json",
			"--auth",
			"file:" + customTokenPath,
			"--listen",
			"127.0.0.1:0",
		})
		if code != 0 {
			t.Fatalf("unexpected exit code: got=%d stderr=%s", code, stderr.String())
		}
	})

	lines := scanLines(t, stdout.String())
	if len(lines) == 0 {
		t.Fatal("expected NDJSON output")
	}

	seenEvents := map[string]bool{}
	var connectionData map[string]interface{}
	for _, line := range lines {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid NDJSON line: %q err=%v", line, err)
		}

		eventName, _ := event["event"].(string)
		seenEvents[eventName] = true

		if eventName == "connection" {
			data, ok := event["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("connection event data has unexpected shape: %#v", event["data"])
			}
			connectionData = data
		}
	}

	for _, required := range []string{"index_loaded", "server_started", "connection", "scan_progress", "embed_progress"} {
		if !seenEvents[required] {
			t.Fatalf("missing required event: %s", required)
		}
	}

	if connectionData == nil {
		t.Fatal("missing connection event payload")
	}
	if connectionData["token_source"] != "file" {
		t.Fatalf("unexpected connection token_source: %#v", connectionData["token_source"])
	}
	if connectionData["token_file"] != customTokenAbs {
		t.Fatalf("unexpected connection token_file: got=%#v want=%#v", connectionData["token_file"], customTokenAbs)
	}
}

func TestUpReturnsExitCode4OnBindFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("DIR2MCP_AUTH_TOKEN", "")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve listener: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewAppWithIO(&stdout, &stderr)

	var code int
	withWorkingDir(t, tmp, func() {
		code = app.RunWithContext(context.Background(), []string{
			"up",
			"--listen",
			listener.Addr().String(),
		})
	})

	if code != 4 {
		t.Fatalf("unexpected exit code: got=%d want=4 stderr=%s", code, stderr.String())
	}
}

func TestUpReturnsExitCode6OnIngestionFatal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MISTRAL_API_KEY", "test-key")
	t.Setenv("DIR2MCP_AUTH_TOKEN", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := cli.NewAppWithIOAndHooks(&stdout, &stderr, cli.RuntimeHooks{
		NewIngestor: func(cfg config.Config, st model.Store) model.Ingestor {
			return failingIngestor{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var code int
	withWorkingDir(t, tmp, func() {
		code = app.RunWithContext(ctx, []string{
			"up",
			"--listen",
			"127.0.0.1:0",
			"--json",
		})
	})

	if code != 6 {
		t.Fatalf("unexpected exit code: got=%d want=6 stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ingestion failed") {
		t.Fatalf("expected ingestion error in stderr, got: %s", stderr.String())
	}
}

func scanLines(t *testing.T, text string) []string {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(text))
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}
	return lines
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()

	cwdMu.Lock()
	defer cwdMu.Unlock()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	fn()
}

type failingIngestor struct{}

func (f failingIngestor) Run(ctx context.Context) error {
	_ = ctx
	return errors.New("forced ingest failure")
}

func (f failingIngestor) Reindex(ctx context.Context) error {
	_ = ctx
	return nil
}
