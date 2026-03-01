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

// when both inactivity and max lifetime are explicitly zero the server should
// fall back to the hard‑coded defaults (sessionTTL / sessionCleanupInterval).
// This regression test ensures we don't accidentally treat zero as a valid
// timeout value, matching the behaviour of resolveSessionTimeouts().
func TestSessionSweepInterval_ZeroConfigDefaults(t *testing.T) {
	cfg := config.Default()
	// explicitly clear. Default() itself is non‑zero, so this simulates
	// a user-provided config that zeroes both values.
	cfg.SessionInactivityTimeout = 0
	cfg.SessionMaxLifetime = 0
	s := NewServer(cfg, nil)

	got := s.sessionSweepInterval()
	want := 30 * time.Minute // same as TestSessionSweepInterval_Defaults
	if got != want {
		t.Fatalf("sessionSweepInterval()=%v want=%v", got, want)
	}
}

// when inactivity timeout is zero but max lifetime is set, the sweep
// interval should be derived solely from the max lifetime.  choose a small
// max lifetime so that half the value would be <1s and therefore the
// one‑second floor is applied.
func TestSessionSweepInterval_ZeroInactivityUsesMaxLifetime(t *testing.T) {
	cfg := config.Default()
	cfg.SessionInactivityTimeout = 0
	cfg.SessionMaxLifetime = 1 * time.Second
	s := NewServer(cfg, nil)

	got := s.sessionSweepInterval()
	want := time.Second // half of 1s would be 500ms, floor to 1s
	if got != want {
		t.Fatalf("sessionSweepInterval()=%v want=%v", got, want)
	}
}
