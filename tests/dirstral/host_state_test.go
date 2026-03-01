package test

import (
	"path/filepath"
	"testing"

	"dir2mcp/internal/dirstral/host"
)

func TestHostStateRoundTrip(t *testing.T) {
	setTestConfigDir(t)

	s := host.State{PID: 12345, StartedAt: "now", MCPURL: "http://127.0.0.1:8087/mcp"}
	if err := host.SaveState(s); err != nil {
		t.Fatalf("save state: %v", err)
	}
	defer func() {
		if err := host.ClearState(); err != nil {
			t.Errorf("clear state: %v", err)
		}
	}()

	loaded, err := host.LoadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.PID != s.PID || loaded.MCPURL != s.MCPURL {
		t.Fatalf("unexpected loaded state: %#v", loaded)
	}
}

func setTestConfigDir(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, ".config"))
}
