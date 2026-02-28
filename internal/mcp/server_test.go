package mcp

import "testing"

func TestNewServer_ValidatesMCPPath(t *testing.T) {
	if _, err := NewServer(ServerOptions{McpPath: ""}); err == nil {
		t.Fatal("expected error for empty mcp path")
	}
	if _, err := NewServer(ServerOptions{McpPath: "mcp"}); err == nil {
		t.Fatal("expected error for relative mcp path")
	}
	if _, err := NewServer(ServerOptions{McpPath: "/mcp"}); err != nil {
		t.Fatalf("expected valid mcp path, got error: %v", err)
	}
}
