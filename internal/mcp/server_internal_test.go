package mcp

import (
	"testing"
	"time"

	"dir2mcp/internal/config"
)

func TestSessionSweepInterval_Defaults(t *testing.T) {
	cfg := config.Default()
	s := NewServer(cfg, nil)

	got := s.sessionSweepInterval()
	want := 30 * time.Minute // min(24h, 1h)/2
	if got != want {
		t.Fatalf("sessionSweepInterval()=%v want=%v", got, want)
	}
}

func TestSessionSweepInterval_UsesSmallerConfiguredTimeout(t *testing.T) {
	cfg := config.Default()
	cfg.SessionInactivityTimeout = 10 * time.Minute
	cfg.SessionMaxLifetime = 0
	s := NewServer(cfg, nil)

	got := s.sessionSweepInterval()
	want := 5 * time.Minute // inactivity/2
	if got != want {
		t.Fatalf("sessionSweepInterval()=%v want=%v", got, want)
	}
}

func TestSessionSweepInterval_UsesSmallestWindowWithFloor(t *testing.T) {
	cfg := config.Default()
	cfg.SessionInactivityTimeout = 1500 * time.Millisecond
	cfg.SessionMaxLifetime = 2 * time.Second
	s := NewServer(cfg, nil)

	got := s.sessionSweepInterval()
	want := time.Second // floor at 1s
	if got != want {
		t.Fatalf("sessionSweepInterval()=%v want=%v", got, want)
	}
}
