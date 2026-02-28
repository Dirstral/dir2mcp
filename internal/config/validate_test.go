package config

import (
	"strings"
	"testing"
)

// TestValidate_NilConfigYieldsActionableOutput verifies nil config returns error, not panic.
func TestValidate_NilConfigYieldsActionableOutput(t *testing.T) {
	err := Validate(nil, true)
	if err == nil {
		t.Fatal("expected error when config is nil")
	}
	if !strings.Contains(err.Error(), "CONFIG_INVALID") {
		t.Errorf("error should contain CONFIG_INVALID, got: %s", err.Error())
	}
}

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
	msg := err.Error()
	if !strings.Contains(msg, "CONFIG_INVALID") {
		t.Errorf("error should contain CONFIG_INVALID, got: %s", msg)
	}
}

// TestValidate_InvalidEnumRejected verifies invalid enum values fail validation.
func TestValidate_InvalidEnumRejected(t *testing.T) {
	cfg := Default()
	cfg.Mistral.APIKey = "test-key"

	cfg.Ingest.PDF.Mode = "invalid"
	err := Validate(&cfg, true)
	if err == nil {
		t.Fatal("expected error for invalid ingest.pdf.mode")
	}
	if !strings.Contains(err.Error(), "ingest.pdf.mode") {
		t.Errorf("error should mention ingest.pdf.mode, got: %s", err.Error())
	}

	cfg = Default()
	cfg.Mistral.APIKey = "test-key"
	cfg.STT.Provider = "unknown"
	err = Validate(&cfg, true)
	if err == nil {
		t.Fatal("expected error for invalid stt.provider")
	}
	if !strings.Contains(err.Error(), "stt.provider") {
		t.Errorf("error should mention stt.provider, got: %s", err.Error())
	}
}
