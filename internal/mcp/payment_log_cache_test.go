package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dir2mcp/internal/config"
)

// badWriter implements io.Writer but always returns an error. It is used to
// trigger a flush failure inside Server.Close during testing. We expose the
// underlying error as a sentinel so tests can use errors.Is to verify that the
// joined error contains it.

var errWriteFailure = errors.New("write failure")

type badWriter struct{}

func (badWriter) Write(p []byte) (int, error) { return 0, errWriteFailure }

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
		if entry["event"] == nil {
			t.Fatalf("line %d missing event field", i+1)
		}
	}
}

func TestCloseErrorsPropagated(t *testing.T) {
	// create a server and manually assign a writer that will return an error
	// when flushed and a file that is already closed so Close() on it fails.
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.AuthMode = "none"

	s := NewServer(cfg, nil)

	// failing writer: underlying writer always returns an error.  Write a
	// byte so that Flush() will attempt to write it and trigger the error.
	s.paymentLogWriter = bufio.NewWriter(badWriter{})
	_, _ = s.paymentLogWriter.Write([]byte("x"))

	// prepare a file and close it immediately to force a close error later
	f, err := os.Create(filepath.Join(tmp, "dummy"))
	if err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close dummy file: %v", err)
	}
	// f is now closed; calling Close again yields an error
	s.paymentLogFile = f

	err = s.Close()
	if err == nil {
		t.Fatalf("expected error from Close when flush and file close fail")
	}
	// Ensure both underlying errors are present via errors.Is.
	if !errors.Is(err, errWriteFailure) {
		t.Fatalf("joined error did not contain flush error: %v", err)
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("joined error did not contain file close error: %v", err)
	}
	// Verify that the returned error implements Unwrap() []error so we know it's
	// actually a joined error and that there are two components.
	if je, ok := err.(interface{ Unwrap() []error }); ok {
		if len(je.Unwrap()) != 2 {
			t.Fatalf("expected two errors from joined error, got %d", len(je.Unwrap()))
		}
	} else {
		t.Fatalf("expected joined error type, got %T", err)
	}
	if s.paymentLogWriter != nil || s.paymentLogFile != nil {
		t.Fatalf("expected writer and file fields to be cleared after Close")
	}
}
