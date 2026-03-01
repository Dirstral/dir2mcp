package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"dir2mcp/internal/dirstral/host"
)

func TestSmokeLighthouseLifecycleWithFakeDir2MCP(t *testing.T) {
	setTestConfigDir(t)
	installFakeDir2MCPOnPath(t)

	listen := "127.0.0.1:" + reserveFreePort(t)
	errCh := make(chan error, 1)
	go func() {
		errCh <- host.Up(context.Background(), host.UpOptions{Listen: listen, MCPPath: "/mcp"})
	}()

	waitFor(t, 3*time.Second, func() bool {
		_, err := host.LoadState()
		return err == nil
	})

	waitFor(t, 3*time.Second, func() bool {
		return host.Status() == nil
	})

	if err := host.Down(); err != nil {
		t.Fatalf("down failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("up returned error after down: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for lighthouse up to exit")
	}

	if _, err := host.LoadState(); err == nil {
		t.Fatalf("expected host state to be cleared")
	}
}

func installFakeDir2MCPOnPath(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	shimPath := filepath.Join(binDir, "dir2mcp")
	shim := fmt.Sprintf("#!/bin/sh\nGO_WANT_HELPER_PROCESS=dir2mcp exec %q -test.run=TestHelperProcessDir2MCPBinary -- \"$@\"\n", os.Args[0])
	if err := os.WriteFile(shimPath, []byte(shim), 0o755); err != nil {
		t.Fatalf("write dir2mcp shim: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func reserveFreePort(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return port
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestHelperProcessDir2MCPBinary(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "dir2mcp" {
		return
	}

	if err := runFakeDir2MCPBinary(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func runFakeDir2MCPBinary() error {
	args := os.Args
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}

	listen := "127.0.0.1:8087"
	mcpPath := "/mcp"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			if i+1 < len(args) {
				listen = args[i+1]
				i++
			}
		case "--mcp-path":
			if i+1 < len(args) {
				mcpPath = args[i+1]
				i++
			}
		}
	}
	if !strings.HasPrefix(mcpPath, "/") {
		mcpPath = "/" + mcpPath
	}

	mux := http.NewServeMux()
	const sessionID = "sess-smoke-host"
	mux.HandleFunc(mcpPath, func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		id := req["id"]

		switch method {
		case "initialize":
			w.Header().Set("MCP-Session-Id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"capabilities": map[string]any{"tools": map[string]any{}},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if r.Header.Get("MCP-Session-Id") != sessionID {
				http.Error(w, "unknown session", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"tools": []map[string]any{{"name": "dir2mcp.search", "description": "search"}},
				},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	server := &http.Server{Addr: listen, Handler: mux}
	sigCh := make(chan os.Signal, 1)
	signalNotify(sigCh)
	defer signalStop(sigCh)

	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	for {
		select {
		case <-sigCh:
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			_ = server.Shutdown(ctx)
			cancel()
			return nil
		case err := <-errCh:
			return err
		}
	}
}

func signalNotify(c chan<- os.Signal) {
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
}

func signalStop(c chan<- os.Signal) {
	signal.Stop(c)
}
