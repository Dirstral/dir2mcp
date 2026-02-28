package config

import (
	"strings"
	"testing"
)

// TestValidate_MissingConfigYieldsActionableOutput verifies missing required config
// returns error with CONFIG_INVALID and remediation (issue #10).
func TestValidate_MissingConfigYieldsActionableOutput(t *testing.T) {
	cfg := Default()
	cfg.Mistral.APIKey = ""

	err := Validate(&cfg, true)
	if err == nil {
		t.Fatal("expected error when API key missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "CONFIG_INVALID") {
		t.Errorf("error should contain CONFIG_INVALID, got: %s", msg)
	}
	if !strings.Contains(msg, "MISTRAL_API_KEY") {
		t.Errorf("error should mention MISTRAL_API_KEY, got: %s", msg)
	}
	if !strings.Contains(msg, "Set env") {
		t.Errorf("error should be actionable (Set env), got: %s", msg)
	}
	if !strings.Contains(msg, "dir2mcp config init") {
		t.Errorf("error should suggest config init, got: %s", msg)
	}
}

// TestValidate_PlaceholderTreatedAsMissing verifies ${MISTRAL_API_KEY} in file is treated as missing.
func TestValidate_PlaceholderTreatedAsMissing(t *testing.T) {
	cfg := Default()
	cfg.Mistral.APIKey = "${MISTRAL_API_KEY}"

	err := Validate(&cfg, true)
	if err == nil {
		t.Fatal("expected error when key is placeholder")
	}
}
