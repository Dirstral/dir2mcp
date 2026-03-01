package mcp

import (
	"testing"
	"time"

	"dir2mcp/internal/config"
)

// The six legacy tests below described various combinations of the
// configured session timeouts.  They're all very similar; exercising the
// same s.sessionSweepInterval logic with slightly different config values.
// Combining them into a single table-driven test reduces boilerplate while
// keeping the individual expectations named and diagnostic-friendly.
func TestSessionSweepInterval(t *testing.T) {
	cases := []struct {
		name           string
		setInactivity  bool
		inactivity     time.Duration
		setMaxLifetime bool
		maxLifetime    time.Duration
		want           time.Duration
	}{
		{
			name: "defaults",
			// leave both values at whatever config.Default() gives us
			want: 30 * time.Minute, // min(24h, 1h)/2
		},
		{
			name:           "smaller inactivity",
			setInactivity:  true,
			inactivity:     10 * time.Minute,
			setMaxLifetime: true,
			maxLifetime:    0,
			want:           5 * time.Minute, // inactivity/2
		},
		{
			name:           "max lifetime smaller",
			setInactivity:  true,
			inactivity:     1 * time.Hour,
			setMaxLifetime: true,
			maxLifetime:    10 * time.Minute,
			want:           5 * time.Minute, // maxLifetime/2
		},
		{
			name:           "floorApplied",
			setInactivity:  true,
			inactivity:     1500 * time.Millisecond,
			setMaxLifetime: true,
			maxLifetime:    2 * time.Second,
			want:           time.Second, // floor at 1s
		},
		{
			name:           "explicit zeroes",
			setInactivity:  true,
			inactivity:     0,
			setMaxLifetime: true,
			maxLifetime:    0,
			want:           30 * time.Minute, // fallback to defaults
		},
		{
			name:           "zero inactivity uses max",
			setInactivity:  true,
			inactivity:     0,
			setMaxLifetime: true,
			maxLifetime:    1 * time.Second,
			want:           time.Second, // half of 1s floored
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			if tc.setInactivity {
				cfg.SessionInactivityTimeout = tc.inactivity
			}
			if tc.setMaxLifetime {
				cfg.SessionMaxLifetime = tc.maxLifetime
			}
			s := NewServer(cfg, nil)

			got := s.sessionSweepInterval()
			if got != tc.want {
				t.Fatalf("%s: sessionSweepInterval()=%v want=%v", tc.name, got, tc.want)
			}
		})
	}
}
