package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"dir2mcp/internal/dirstral/breeze"
	"dir2mcp/internal/protocol"
)

func TestSmokeBreezeJSONOverStdio(t *testing.T) {
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	defer func() {
		_ = inR.Close()
		_ = inW.Close()
		_ = outR.Close()
		_ = outW.Close()
	}()

	oldIn := os.Stdin
	oldOut := os.Stdout
	os.Stdin = inR
	os.Stdout = outW
	defer func() {
		os.Stdin = oldIn
		os.Stdout = oldOut
	}()

	_, _ = inW.WriteString("/list src\n/quit\n")
	_ = inW.Close()

	endpoint := fmt.Sprintf("/usr/bin/env GO_WANT_HELPER_PROCESS=1 %s -test.run=TestHelperProcessBreezeSmokeMCPStdio --", os.Args[0])
	opts := breeze.Options{
		MCPURL:    endpoint,
		Transport: "stdio",
		Model:     "mistral-small-latest",
		JSON:      true,
	}
	if err := breeze.Run(context.Background(), opts); err != nil {
		t.Fatalf("breeze run failed: %v", err)
	}

	_ = outW.Close()
	outBytes := &bytes.Buffer{}
	if _, err := outBytes.ReadFrom(outR); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	events := decodeEvents(t, outBytes.Bytes())
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
	if events[0]["type"] != "session" {
		t.Fatalf("first event must be session, got %v", events[0]["type"])
	}

	var toolEvent map[string]any
	var exitEvent map[string]any
	for _, evt := range events {
		if evt["type"] == "tool_result" {
			toolEvent = evt
		}
		if evt["type"] == "exit" {
			exitEvent = evt
		}
	}
	if toolEvent == nil {
		t.Fatalf("missing tool_result event")
	}
	if exitEvent == nil {
		t.Fatalf("missing exit event")
	}
	data, _ := toolEvent["data"].(map[string]any)
	if data["tool"] != protocol.ToolNameListFiles {
		t.Fatalf("expected %s call, got %v", protocol.ToolNameListFiles, data["tool"])
	}
}

func TestHelperProcessBreezeSmokeMCPStdio(t *testing.T) {
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
			helperWriteJSON(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"tools": []map[string]any{
						{"name": protocol.ToolNameListFiles, "description": "list files"},
						{"name": protocol.ToolNameSearch, "description": "search"},
						{"name": protocol.ToolNameOpenFile, "description": "open file"},
						{"name": protocol.ToolNameStats, "description": "stats"},
						{"name": protocol.ToolNameAsk, "description": "ask"},
					},
				},
			})
		case "tools/call":
			helperWriteJSON(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"isError": false,
					"content": []map[string]any{{"type": "text", "text": "src/main.go"}},
					"structuredContent": map[string]any{
						"files": []map[string]any{{"rel_path": "src/main.go", "kind": "text"}},
					},
				},
			})
		default:
			helperWriteJSON(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "method not found"}})
		}
	}
}
