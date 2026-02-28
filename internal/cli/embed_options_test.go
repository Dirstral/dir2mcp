package cli

import (
	"testing"
)

func TestParseUpOptions_EmbedModelFlags(t *testing.T) {
	global := globalOptions{}
	opts, err := parseUpOptions(global, []string{"--embed-model-text", "foo", "--embed-model-code", "bar"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if opt := opts.embedModelText; opt != "foo" {
		t.Errorf("expected embedModelText foo, got %q", opt)
	}
	if opt := opts.embedModelCode; opt != "bar" {
		t.Errorf("expected embedModelCode bar, got %q", opt)
	}
}
