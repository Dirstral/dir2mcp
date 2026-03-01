package x402

import "testing"

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
		got := NormalizeMode(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeMode(%q) = %q; want %q", tc.input, got, tc.want)
		}
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

	invalid := []string{"", "", "foo", "123", "maybe"}
	for _, v := range invalid {
		if IsModeValid(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}
