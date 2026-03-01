package tests

import (
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
			t.Fatalf("X402.FacilitatorURL=%q", cfg.X402.FacilitatorURL)
		}
		if cfg.X402.ResourceBaseURL != "https://resource.example.com" {
			t.Fatalf("X402.ResourceBaseURL=%q", cfg.X402.ResourceBaseURL)
		}
		if cfg.X402.Network != "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp" {
			t.Fatalf("X402.Network=%q", cfg.X402.Network)
		}
	})
}

func TestValidateX402_StrictRequiresCAIP2Network(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "required"
	cfg.X402.ToolsCallEnabled = true
	cfg.X402.FacilitatorURL = "https://facilitator.example.com"
	cfg.X402.ResourceBaseURL = "https://resource.example.com"
	cfg.X402.PriceAtomic = "1000"
	cfg.X402.Scheme = "exact"
	cfg.X402.Network = "not-a-caip2-network"
	cfg.X402.Asset = "asset"
	cfg.X402.PayTo = "payto"

	if err := cfg.ValidateX402(true); err == nil {
		t.Fatal("expected strict x402 validation error for invalid network")
	}
}
