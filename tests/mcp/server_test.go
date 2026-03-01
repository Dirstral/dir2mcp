package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dir2mcp/internal/config"
	"dir2mcp/internal/mcp"
)

func TestMCPInitialize_AllowsOriginWithPortWhenAllowlistOmitsPort(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.AllowedOrigins = []string{"http://localhost"}

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, err := http.NewRequest(http.MethodPost, server.URL+cfg.MCPPath, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}
	if strings.TrimSpace(resp.Header.Get("MCP-Session-Id")) == "" {
		t.Fatal("expected MCP-Session-Id header on initialize response")
	}
}

func TestSessionExpiration_InactivityHeader(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	// keep values small but with enough buffer to avoid timer-granularity
	// flakes in CI environments.
	cfg.SessionInactivityTimeout = 20 * time.Millisecond
	cfg.SessionMaxLifetime = 0

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	// initialize to obtain a session id
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, server.URL+cfg.MCPPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do initialize: %v", err)
	}
	sessionID := resp.Header.Get("MCP-Session-Id")
	_ = resp.Body.Close()

	// wait comfortably past inactivity window
	time.Sleep(100 * time.Millisecond)

	// send a non-initialize request so session validation runs
	body2 := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	req2, _ := http.NewRequest(http.MethodPost, server.URL+cfg.MCPPath, strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("MCP-Session-Id", sessionID)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("do second req: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on expired session, got %d", resp2.StatusCode)
	}
	if resp2.Header.Get("X-MCP-Session-Expired") != "inactivity" {
		t.Fatalf("expected inactivity header, got %q", resp2.Header.Get("X-MCP-Session-Expired"))
	}
}

func TestSessionExpiration_MaxLifetimeHeader(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	// keep values small but with enough buffer to avoid timer-granularity
	// flakes in CI environments.
	cfg.SessionInactivityTimeout = 1 * time.Hour
	cfg.SessionMaxLifetime = 20 * time.Millisecond

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, server.URL+cfg.MCPPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do initialize: %v", err)
	}
	sessionID := resp.Header.Get("MCP-Session-Id")
	_ = resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	body2 := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	req2, _ := http.NewRequest(http.MethodPost, server.URL+cfg.MCPPath, strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("MCP-Session-Id", sessionID)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("do second req: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on expired session, got %d", resp2.StatusCode)
	}
	if resp2.Header.Get("X-MCP-Session-Expired") != "max-lifetime" {
		t.Fatalf("expected max-lifetime header, got %q", resp2.Header.Get("X-MCP-Session-Expired"))
	}
}

func TestMCPInitialize_RejectsMissingJSONRPCVersion(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	body := `{"id":1,"method":"initialize","params":{}}`
	req, err := http.NewRequest(http.MethodPost, server.URL+cfg.MCPPath, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusBadRequest {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusBadRequest, string(payload))
	}

	var envelope struct {
		Error struct {
			Data struct {
				Code string `json:"code"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Data.Code != "INVALID_FIELD" {
		t.Fatalf("canonical code=%q want=%q", envelope.Error.Data.Code, "INVALID_FIELD")
	}
}
