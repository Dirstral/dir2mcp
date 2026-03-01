package test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"dir2mcp/internal/dirstral/mcp"
	"dir2mcp/internal/protocol"
)

func TestMCPClientStdioInitializeAndCall(t *testing.T) {
	endpoint := fmt.Sprintf("/usr/bin/env GO_WANT_HELPER_PROCESS=1 %s -test.run=TestHelperProcessMCPStdio --", os.Args[0])
	client := mcp.NewWithTransport(endpoint, "stdio", false)
	defer func() {
		_ = client.Close()
	}()

	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize stdio: %v", err)
	}
	if client.SessionID() != "stdio" {
		t.Fatalf("expected stdio session id, got %q", client.SessionID())
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) == 0 || tools[0].Name != protocol.ToolNameAsk {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	res, err := client.CallTool(context.Background(), protocol.ToolNameAsk, map[string]any{"question": "hello", "k": 8})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if got, _ := res.StructuredContent["answer"].(string); got != "ok from stdio" {
		t.Fatalf("unexpected answer: %q", got)
	}
}

func TestHelperProcessMCPStdio(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		payload, err := helperReadMessage(reader)
		if err != nil {
			os.Exit(0)
		}
		var req map[string]any
		if err := json.Unmarshal(payload, &req); err != nil {
			os.Exit(1)
		}
		method, _ := req["method"].(string)
		id := req["id"]
		switch method {
		case "initialize":
			helperWriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"ok": true}})
		case "notifications/initialized":
			continue
		case "tools/list":
			helperWriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"tools": []map[string]any{{"name": protocol.ToolNameAsk, "description": "ask"}}}})
		case "tools/call":
			helperWriteJSON(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"isError":           false,
					"content":           []map[string]any{{"type": "text", "text": "ok from stdio"}},
					"structuredContent": map[string]any{"answer": "ok from stdio"},
				},
			})
		default:
			helperWriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "method not found"}})
		}
	}
}

func helperReadMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &contentLength); err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func helperWriteJSON(v map[string]any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(b))
	_, _ = os.Stdout.Write(b)
}
