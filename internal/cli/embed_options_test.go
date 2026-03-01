package cli

import (
	"testing"
)

func TestParseUpOptions_EmbedModelFlags(t *testing.T) {
	global := globalOptions{}
	opts, err := parseUpOptions(global, []string{"--embed-model-text", "foo", "--embed-model-code", "bar", "--chat-model", "baz"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if opt := opts.embedModelText; opt != "foo" {
		t.Errorf("expected embedModelText foo, got %q", opt)
	}
	if opt := opts.embedModelCode; opt != "bar" {
		t.Errorf("expected embedModelCode bar, got %q", opt)
	}
	if opt := opts.chatModel; opt != "baz" {
		t.Errorf("expected chatModel baz, got %q", opt)
	}
}

func TestParseUpOptions_X402TokenFlags(t *testing.T) {
	global := globalOptions{}
	opts, err := parseUpOptions(global, []string{"--x402-facilitator-token-file", "path/to/token", "--x402-facilitator-token", "flagval"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if opts.x402FacilitatorTokenFile != "path/to/token" {
		t.Errorf("expected x402FacilitatorTokenFile path/to/token, got %q", opts.x402FacilitatorTokenFile)
	}
	if opts.x402FacilitatorToken != "flagval" {
		t.Errorf("expected x402FacilitatorToken flagval, got %q", opts.x402FacilitatorToken)
	}
}
