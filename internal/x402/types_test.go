package x402

import (
	"fmt"
	"testing"
)

func TestNormalizeMode(t *testing.T) {
	t.Parallel()

	cases := []struct{ input, want string }{
		{"", ModeOff},              // blank defaults to off
		{"Off", ModeOff},           // case-insensitive
		{"  ON  ", ModeOn},         // whitespace trimmed
		{"REQUIRED", ModeRequired}, // uppercase required
		{"unknown", ModeOff},       // unrecognised -> off
		{"FooBar", ModeOff},        // any garbage maps to off
	}

	for _, tc := range cases {
		name := tc.input
		if name == "" {
			name = "<empty>"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeMode(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeMode(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsModeValid(t *testing.T) {
	t.Parallel()

	valid := []string{"off", "ON", " required ", "On"}
	for _, v := range valid {
		if !IsModeValid(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}

	invalid := []string{"", " ", "foo", "123", "maybe"}
	for _, v := range invalid {
		if IsModeValid(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestIsModeEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  bool
	}{
		{"off", false},
		{"on", true},
		{"required", true},
		{"  ON  ", true},
		{"unknown", false},
		{"", false},
	}

	for _, tc := range cases {
		got := IsModeEnabled(tc.input)
		if got != tc.want {
			t.Errorf("IsModeEnabled(%q) = %v; want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsCAIP2Network(t *testing.T) {
	t.Parallel()

	valid := []string{
		"eip155:1",
		"solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
		"foo:bar",
		"a:1",
	}
	for _, v := range valid {
		if !IsCAIP2Network(v) {
			t.Errorf("expected %q to be valid CAIP-2 network", v)
		}
	}

	invalid := []string{
		"", ":", "no-colon", "too:many:parts", "UPPER:case",
		"ns!:ref", "ns:ref$", "toolongnamespaceabcdefghijklmnopqrstuvwxyz0123456789",
		"ns:", ":ref",
	}
	for _, v := range invalid {
		if IsCAIP2Network(v) {
			t.Errorf("expected %q to be invalid CAIP-2 network", v)
		}
	}
}

func TestFacilitatorError(t *testing.T) {
	t.Parallel()

	err := &FacilitatorError{Code: "X", Message: "msg"}
	if err.Error() != "X: msg" {
		t.Errorf("unexpected error string %q", err.Error())
	}
	err = &FacilitatorError{Code: "X"}
	if err.Error() != "X" {
		t.Errorf("unexpected error string %q", err.Error())
	}
	err = &FacilitatorError{Message: "msg"}
	if err.Error() != "msg" {
		t.Errorf("unexpected error string %q", err.Error())
	}
	err = &FacilitatorError{}
	if err.Error() != "facilitator request failed" {
		t.Errorf("unexpected error string %q", err.Error())
	}

	if (&FacilitatorError{Cause: fmt.Errorf("cause")}).Unwrap() == nil {
		t.Error("expected Non-nil Unwrap result")
	}
	// also verify that an error with no Cause returns nil from Unwrap
	if (&FacilitatorError{}).Unwrap() != nil {
		t.Error("expected nil Unwrap result when Cause is nil")
	}
}
