package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/tests/testutil"
)

func TestLoad_EnvOverridesX402(t *testing.T) {
	tmp := t.TempDir()

	testutil.WithWorkingDir(t, tmp, func() {
		t.Setenv("DIR2MCP_X402_MODE", "on")
		t.Setenv("DIR2MCP_X402_FACILITATOR_URL", "https://facilitator.example.com")
		t.Setenv("DIR2MCP_X402_RESOURCE_BASE_URL", "https://resource.example.com")
		t.Setenv("DIR2MCP_X402_NETWORK", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp")
		t.Setenv("DIR2MCP_X402_ASSET", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
		t.Setenv("DIR2MCP_X402_PAY_TO", "8N5A4rQU8vJrQmH3iiA7kE4m1df4WeyueXQqGb4G9tTj")

		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.X402.Mode != "on" {
			t.Fatalf("X402.Mode=%q want=%q", cfg.X402.Mode, "on")
		}
		if cfg.X402.FacilitatorURL != "https://facilitator.example.com" {
			t.Fatalf("X402.FacilitatorURL=%q want=%q", cfg.X402.FacilitatorURL, "https://facilitator.example.com")
		}
		if cfg.X402.ResourceBaseURL != "https://resource.example.com" {
			t.Fatalf("X402.ResourceBaseURL=%q want=%q", cfg.X402.ResourceBaseURL, "https://resource.example.com")
		}
		if cfg.X402.Network != "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp" {
			t.Fatalf("X402.Network=%q want=%q", cfg.X402.Network, "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp")
		}
		if cfg.X402.Asset != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" {
			t.Fatalf("X402.Asset=%q want=%q", cfg.X402.Asset, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
		}
		if cfg.X402.PayTo != "8N5A4rQU8vJrQmH3iiA7kE4m1df4WeyueXQqGb4G9tTj" {
			t.Fatalf("X402.PayTo=%q want=%q", cfg.X402.PayTo, "8N5A4rQU8vJrQmH3iiA7kE4m1df4WeyueXQqGb4G9tTj")
		}
	})
}

func TestLoad_EnvOverridesX402_EmptyIgnored(t *testing.T) {
	tmp := t.TempDir()

	testutil.WithWorkingDir(t, tmp, func() {
		// start with a config file specifying non-empty values
		yaml := `x402_facilitator_url: https://facilitator.example.com
x402_resource_base_url: https://resource.example.com
` // token is sensitive and ignored by config file
		writeFile(t, filepath.Join(tmp, ".dir2mcp.yaml"), yaml)

		// set environment variables to empty or whitespace
		t.Setenv("DIR2MCP_X402_FACILITATOR_URL", "")
		t.Setenv("DIR2MCP_X402_FACILITATOR_TOKEN", "  ")
		t.Setenv("DIR2MCP_X402_RESOURCE_BASE_URL", "   ")

		// load config file so that file values are applied
		cfg, err := config.Load(".dir2mcp.yaml")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		// blank env values should not override config file values (or default for token)
		if cfg.X402.FacilitatorURL != "https://facilitator.example.com" {
			t.Fatalf("expected facilitator URL to remain unchanged, got %q", cfg.X402.FacilitatorURL)
		}
		if cfg.X402.ResourceBaseURL != "https://resource.example.com" {
			t.Fatalf("expected resource base URL to remain unchanged, got %q", cfg.X402.ResourceBaseURL)
		}
		if cfg.X402.FacilitatorToken != "" {
			t.Fatalf("expected facilitator token to stay empty, got %q", cfg.X402.FacilitatorToken)
		}
	})
}

func TestValidateX402_ModeWithToolsDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "required" // mode says we need X402
	cfg.X402.ToolsCallEnabled = false
	// other fields irrelevant because validation should fail early
	err := cfg.ValidateX402(true)
	if err == nil {
		t.Fatal("expected validation error when tools call disabled but mode is not off")
	}
	if !strings.Contains(err.Error(), "ToolsCallEnabled") {
		t.Fatalf("error message did not mention ToolsCallEnabled: %v", err)
	}
}

func TestValidateX402_InvalidNetwork(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "required" // enable validation path
	cfg.X402.ToolsCallEnabled = true
	cfg.X402.FacilitatorURL = "https://facilitator.example.com"
	cfg.X402.ResourceBaseURL = "https://resource.example.com"
	cfg.X402.FacilitatorToken = "token"
	cfg.X402.PriceAtomic = "1000"
	cfg.X402.Scheme = "exact"
	cfg.X402.Network = "not-a-caip2-network"
	cfg.X402.Asset = "asset"
	cfg.X402.PayTo = "payto"

	err := cfg.ValidateX402(true)
	if err == nil {
		t.Fatal("expected strict x402 validation error for invalid network")
	}
	if !strings.Contains(err.Error(), "CAIP-2") {
		t.Fatalf("expected CAIP-2 validation error, got: %v", err)
	}
}

func TestValidateX402_PriceMustBePositiveInteger(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "required"
	cfg.X402.ToolsCallEnabled = true
	cfg.X402.FacilitatorURL = "https://facilitator.example.com"
	cfg.X402.ResourceBaseURL = "https://resource.example.com"
	cfg.X402.FacilitatorToken = "token"
	cfg.X402.Scheme = "exact"
	cfg.X402.Network = "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp" // valid CAIP-2
	cfg.X402.Asset = "asset"
	cfg.X402.PayTo = "payto"

	// iterate through a few invalid price strings, running each in a subtest
	for _, tc := range []struct {
		name string
		bad  string
	}{
		{"empty", ""},
		{"non-integer", "abc"},
		{"negative", "-100"},
	} {
		// capture range variable for the closure
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// copy config so subtests don't mutate shared state
			localCfg := cfg
			localCfg.X402.PriceAtomic = tc.bad
			err := localCfg.ValidateX402(true)
			if err == nil {
				t.Fatalf("price %q should have failed validation", tc.bad)
			}
			if tc.bad == "" {
				if !strings.Contains(err.Error(), "required") {
					t.Fatalf("empty price produced wrong error: %v", err)
				}
			} else {
				if !strings.Contains(err.Error(), "positive integer") {
					t.Fatalf("unexpected error message for price %q: %v", tc.bad, err)
				}
			}
		})
	}

	cfg.X402.PriceAtomic = "0"
	if err := cfg.ValidateX402(true); err == nil {
		t.Fatal("expected price 0 to be rejected")
	}
	cfg.X402.PriceAtomic = "12345"
	if err := cfg.ValidateX402(true); err != nil {
		t.Fatalf("expected positive price to be valid, got %v", err)
	}
}

func TestValidateX402_InvalidScheme(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "required"
	cfg.X402.ToolsCallEnabled = true
	cfg.X402.FacilitatorURL = "https://facilitator.example.com"
	cfg.X402.ResourceBaseURL = "https://resource.example.com"
	cfg.X402.FacilitatorToken = "token"
	cfg.X402.PriceAtomic = "1000"
	cfg.X402.Scheme = "not-allowed"
	cfg.X402.Network = "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	cfg.X402.Asset = "asset"
	cfg.X402.PayTo = "payto"

	err := cfg.ValidateX402(true)
	if err == nil {
		t.Fatal("expected strict x402 validation error for invalid scheme")
	}
	if !strings.Contains(err.Error(), "one of") {
		t.Fatalf("unexpected error message for scheme: %v", err)
	}

	cfg.X402.Scheme = "EXACT"
	if err := cfg.ValidateX402(true); err != nil {
		t.Fatalf("expected uppercase exact to be accepted, got %v", err)
	}

	cfg.X402.Scheme = " UpTo "
	if err := cfg.ValidateX402(true); err != nil {
		t.Fatalf("expected spaced/upper upto to be accepted, got %v", err)
	}
}
