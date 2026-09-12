package tests

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dirstral/dir2mcp/internal/config"
)

// The §8.6.13 partial-transcript floor (#961) is a pair of keys, mirroring the
// §8.2.1 language floor: media.stt.min_coverage declares it and
// media.stt.on_partial_transcript answers it.

// TestMediaSTTPartialTranscriptFloor_Defaults pins the shipped default: the floor
// is OFF (min_coverage 0) and the action is the fail-open "warn". Recording the
// coverage is mandatory; refusing a transcript on it is opt-in, so an existing
// corpus behaves exactly as it did.
func TestMediaSTTPartialTranscriptFloor_Defaults(t *testing.T) {
	cfg := config.Default()
	if cfg.MediaSTTMinCoverage != 0 {
		t.Errorf("default media.stt.min_coverage = %v, want 0 (the floor ships off)", cfg.MediaSTTMinCoverage)
	}
	if cfg.MediaSTTOnPartialTranscript != "warn" {
		t.Errorf("default media.stt.on_partial_transcript = %q, want warn", cfg.MediaSTTOnPartialTranscript)
	}
}

// TestMediaSTTPartialTranscriptFloor_Validation accepts the valid range and the
// closed action enum, normalizes empty and case, and rejects a fraction outside
// [0,1] or a non-finite one. A silently clamped bad value would leave an operator
// believing in a floor that never trips.
func TestMediaSTTPartialTranscriptFloor_Validation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		minCoverage float64
		action      string
		wantErr     bool
		wantAction  string
	}{
		{name: "empty action normalizes to warn", minCoverage: 0.5, action: "", wantAction: "warn"},
		{name: "case is normalized", minCoverage: 0.5, action: "SKIP", wantAction: "skip"},
		{name: "the bounds are inclusive", minCoverage: 1, action: "skip", wantAction: "skip"},
		{name: "zero is valid and disables the floor", minCoverage: 0, action: "warn", wantAction: "warn"},
		{name: "an unknown action is rejected", minCoverage: 0.5, action: "drop", wantErr: true},
		{name: "a negative fraction is rejected", minCoverage: -0.1, action: "warn", wantErr: true},
		{name: "a fraction above one is rejected", minCoverage: 1.5, action: "warn", wantErr: true},
		{name: "NaN is rejected", minCoverage: math.NaN(), action: "warn", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.MediaSTTMinCoverage = tc.minCoverage
			cfg.MediaSTTOnPartialTranscript = tc.action
			err := cfg.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("min_coverage=%v action=%q validated, want an error", tc.minCoverage, tc.action)
				}
				return
			}
			if err != nil {
				t.Fatalf("min_coverage=%v action=%q: %v", tc.minCoverage, tc.action, err)
			}
			if cfg.MediaSTTOnPartialTranscript != tc.wantAction {
				t.Errorf("on_partial_transcript = %q, want %q", cfg.MediaSTTOnPartialTranscript, tc.wantAction)
			}
		})
	}
}

// TestMediaSTTPartialTranscriptFloor_ParsesFlatAndNestedYAML confirms both config
// forms reach the floor: the flat keys and the nested media.stt block an operator
// actually writes.
func TestMediaSTTPartialTranscriptFloor_ParsesFlatAndNestedYAML(t *testing.T) {
	base := []string{
		"root_dir: /tmp/repo",
		"state_dir: /tmp/repo/.dir2mcp",
	}
	flat := append(append([]string(nil), base...),
		"media_stt_min_coverage: 0.8", "media_stt_on_partial_transcript: skip")
	nested := append(append([]string(nil), base...),
		"media:", "  stt:", "    min_coverage: 0.8", "    on_partial_transcript: skip")

	for name, lines := range map[string][]string{"flat": flat, "nested": nested} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".dir2mcp.yaml")
			writeFile(t, path, strings.Join(lines, "\n")+"\n")
			cfg, err := config.LoadFile(path)
			if err != nil {
				t.Fatalf("LoadFile(%s): %v", name, err)
			}
			if cfg.MediaSTTMinCoverage != 0.8 {
				t.Errorf("%s form: min_coverage = %v, want 0.8", name, cfg.MediaSTTMinCoverage)
			}
			if cfg.MediaSTTOnPartialTranscript != "skip" {
				t.Errorf("%s form: on_partial_transcript = %q, want skip", name, cfg.MediaSTTOnPartialTranscript)
			}
		})
	}
}

// TestMediaSTTPartialTranscriptFloor_RoundTripsThroughSaveLoad confirms the floor
// survives a save/load cycle, so `dir2mcp config` does not silently drop it.
func TestMediaSTTPartialTranscriptFloor_RoundTripsThroughSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dir2mcp.yaml")

	cfg := config.Default()
	cfg.RootDir = "/tmp/repo"
	cfg.StateDir = "/tmp/repo/.dir2mcp"
	cfg.MediaSTTMinCoverage = 0.75
	cfg.MediaSTTOnPartialTranscript = "skip"
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	text := readFileString(t, path)
	if !strings.Contains(text, "media_stt_min_coverage: 0.75") {
		t.Fatalf("saved config missing media_stt_min_coverage:\n%s", text)
	}
	if !strings.Contains(text, "media_stt_on_partial_transcript: skip") {
		t.Fatalf("saved config missing media_stt_on_partial_transcript:\n%s", text)
	}
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.MediaSTTMinCoverage != 0.75 || loaded.MediaSTTOnPartialTranscript != "skip" {
		t.Fatalf("floor did not round-trip: min_coverage=%v action=%q",
			loaded.MediaSTTMinCoverage, loaded.MediaSTTOnPartialTranscript)
	}
}
