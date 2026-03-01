package mcp

import (
	"testing"
	"time"

	"dir2mcp/internal/config"
)

func TestInitPaymentConfig_ModeOnIncompleteConfigDisablesGating(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "on"
	cfg.X402.ToolsCallEnabled = true
	// Intentionally incomplete x402 config.

	s := NewServer(cfg, nil)
	if s.x402Enabled {
		t.Fatal("expected x402 gating to remain disabled for incomplete mode=on config")
	}
}

func TestPrunePaymentOutcomesLocked_AppliesTTLAndCap(t *testing.T) {
	now := time.Now().UTC()

	s := &Server{
		paymentOutcomes: map[string]paymentExecutionOutcome{
			"expired": {UpdatedAt: now.Add(-20 * time.Minute)},
			"a":       {UpdatedAt: now.Add(-4 * time.Minute)},
			"b":       {UpdatedAt: now.Add(-3 * time.Minute)},
			"c":       {UpdatedAt: now.Add(-2 * time.Minute)},
		},
		paymentTTL:      10 * time.Minute,
		paymentMaxItems: 2,
	}

	s.cleanupPaymentOutcomes(now)

	if _, ok := s.paymentOutcomes["expired"]; ok {
		t.Fatal("expected expired payment outcome to be pruned by TTL")
	}
	if len(s.paymentOutcomes) != 2 {
		t.Fatalf("expected 2 outcomes after cap pruning, got %d", len(s.paymentOutcomes))
	}
	if _, ok := s.paymentOutcomes["a"]; ok {
		t.Fatal("expected oldest non-expired outcome to be pruned by cap")
	}
	if _, ok := s.paymentOutcomes["b"]; !ok {
		t.Fatal("expected more recent outcome b to remain")
	}
	if _, ok := s.paymentOutcomes["c"]; !ok {
		t.Fatal("expected most recent outcome c to remain")
	}
}
