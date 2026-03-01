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

	// cases exercise the mutual‑exclusivity and documented precedence between
	// the two token flags. parseUpOptions itself does not read the file, but
	// the command‑line semantics dictate that the file flag wins over a direct
	// token when both are provided.
	//
	// wantDirectSet mirrors the post‑parse value of
	// opts.x402FacilitatorTokenDirectSet.  Rather than signifying that the
	// option "remains set" internally, it simply records whether the flag was
	// supplied on the command line at all.  The parser may still clear the
	// token value later if a file flag takes precedence; we keep the boolean
	// for conflict detection and warning logic.  For example, in the "both"
	// test case the direct token is wiped in favour of the file, but
	// wantDirectSet is true because the user did pass the flag.
	tests := []struct {
		name          string
		args          []string
		wantFile      string
		wantToken     string
		wantDirectSet bool
	}{
		{"file only", []string{"--x402-facilitator-token-file", "path/to/token"}, "path/to/token", "", false},
		{"token only", []string{"--x402-facilitator-token", "flagval"}, "", "flagval", false},
		// both flags present; precedence rules say the file path should win,
		// so parseUpOptions is expected to clear the direct token and record
		// that it was originally set.
		{"both", []string{"--x402-facilitator-token-file", "path/to/token", "--x402-facilitator-token", "flagval"}, "path/to/token", "", true},
		// verify precedence is order-independent by specifying the direct flag
		// before the file flag; semantics should still favor the file flag.
		{"both order reversed", []string{"--x402-facilitator-token", "flagval", "--x402-facilitator-token-file", "path/to/token"}, "path/to/token", "", true},
		{"neither", []string{}, "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseUpOptions(global, tt.args)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if opts.x402FacilitatorTokenFile != tt.wantFile {
				t.Errorf("expected x402FacilitatorTokenFile %q, got %q", tt.wantFile, opts.x402FacilitatorTokenFile)
			}
			if opts.x402FacilitatorToken != tt.wantToken {
				t.Errorf("expected x402FacilitatorToken %q, got %q", tt.wantToken, opts.x402FacilitatorToken)
			}
			if opts.x402FacilitatorTokenDirectSet != tt.wantDirectSet {
				t.Errorf("expected x402FacilitatorTokenDirectSet %v, got %v", tt.wantDirectSet, opts.x402FacilitatorTokenDirectSet)
			}
		})
	}

}
