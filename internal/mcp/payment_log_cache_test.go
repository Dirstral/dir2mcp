package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dir2mcp/internal/config"
)

func TestAppendPaymentLogCachingAndClose(t *testing.T) {
	// create a temp state dir to hold the log
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.StateDir = tmp

	// new server starts with empty paymentLogPath because x402 is disabled;
	// manually initialize it for our test so appendPaymentLog actually does work.
	s := NewServer(cfg, nil)
	s.paymentLogPath = filepath.Join(tmp, "payments", "settlement.log")
	// make sure the directory doesn't yet exist
	if _, err := os.Stat(filepath.Join(tmp, "payments")); !os.IsNotExist(err) {
		t.Fatalf("expected payments directory to be absent")
	}

	// manually append a couple of entries; path should be created and writer cached
	s.appendPaymentLog("evt1", map[string]interface{}{"foo": "bar"})
	s.appendPaymentLog("evt2", map[string]interface{}{"baz": 123})

	// close to flush
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned unexpected error: %v", err)
	}

	// read file and verify two JSON lines
	logPath := filepath.Join(tmp, "payments", "settlement.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrong number of log lines: got %d, want 2", len(lines))
	}
	for i, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d is not valid json: %v", i+1, err)
		}
		if entry["event"] == "" {
			t.Fatalf("line %d missing event field", i+1)
		}
	}
}
