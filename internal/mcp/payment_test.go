package mcp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"dir2mcp/internal/config"
)

func TestInitPaymentConfig_ModeOnIncompleteConfigDisablesGating(t *testing.T) {
	cfg := config.Default()
	cfg.X402.Mode = "on"
	cfg.X402.ToolsCallEnabled = true
	// Intentionally incomplete X402 config for TestInitPaymentConfig_ModeOnIncompleteConfigDisablesGating.
	// We enable mode=on and tools call but omit all required fields such as payment address,
	// network/chain ID, and any merchant ID or API key that NewServer's X402 validation
	// would normally require. The cfg.X402 struct passed to NewServer here has only
	// Mode and ToolsCallEnabled set; nothing else is populated, which should keep
	// x402Enabled=false and gating disabled despite the mode being "on".

	var events []map[string]interface{}
	s := NewServer(cfg, nil, WithEventEmitter(func(level, event string, data interface{}) {
		events = append(events, map[string]interface{}{"level": level, "event": event, "data": data})
	}))
	if s.x402Enabled {
		t.Fatal("expected x402 gating to remain disabled for incomplete mode=on config")
	}
	// ensure a warning was emitted about validation failure
	if len(events) == 0 {
		t.Fatalf("expected warning event when X402 validation fails")
	}
	found := false
	for _, e := range events {
		if e["event"] == "x402_validation_failed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("did not see x402_validation_failed event, events: %+v", events)
	}
}

func TestCleanupPaymentOutcomes_AppliesTTLAndCap(t *testing.T) {
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

func waitForKeyRef(ctx context.Context, s *Server, key string, wantRef int) error {
	for {
		s.execMu.Lock()
		km, ok := s.execKeyMu[key]
		if ok && km.ref == wantRef {
			s.execMu.Unlock()
			return nil
		}
		ref := -1
		if km != nil {
			ref = km.ref
		}
		s.execMu.Unlock()

		select {
		case <-ctx.Done():
			// include current state in error for diagnostic purposes
			return fmt.Errorf("key=%s ok=%v ref=%d: %w", key, ok, ref, ctx.Err())
		case <-time.After(5 * time.Millisecond):
			// continue polling
		}
	}
}

func TestLockForExecutionKey_SerializesAndCleansUpWithRefCounts(t *testing.T) {
	s := &Server{execKeyMu: make(map[string]*keyMutex)}
	key := "same-signature:same-params"

	unlockA := s.lockForExecutionKey(key)

	bAcquired := make(chan struct{})
	bRelease := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		unlockB := s.lockForExecutionKey(key)
		close(bAcquired)
		<-bRelease
		unlockB()
	}()

	// wait until the reference count for the key reaches 2 (A + B)
	{
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := waitForKeyRef(ctx, s, key, 2); err != nil {
			t.Fatalf("expected key mutex ref=2 while B waits: %v", err)
		}
	}

	unlockA()

	select {
	case <-bAcquired:
	case <-time.After(time.Second):
		t.Fatal("B did not acquire lock after A released")
	}

	cAcquired := make(chan struct{})
	cRelease := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		unlockC := s.lockForExecutionKey(key)
		close(cAcquired)
		<-cRelease
		unlockC()
	}()

	// While B holds the lock, C must still be registered as a waiter and remain blocked.
	// Poll under execMu until the ref count reaches 2 (B + C) with a timeout, mirroring
	// the logic we used earlier above when waiting for B to register.
	{
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := waitForKeyRef(ctx, s, key, 2); err != nil {
			t.Fatalf("expected key mutex ref=2 while C waits: %v", err)
		}
	}
	// Now that C is registered, ensure it is still blocked (should not have acquired lock yet).
	select {
	case <-cAcquired:
		t.Fatal("C acquired lock while B still held it; keyed exclusion broken")
	default:
	}

	close(bRelease)
	select {
	case <-cAcquired:
	case <-time.After(time.Second):
		t.Fatal("C did not acquire lock after B released")
	}
	close(cRelease)
	wg.Wait()

	s.execMu.Lock()
	defer s.execMu.Unlock()
	if len(s.execKeyMu) != 0 {
		t.Fatalf("expected key mutex map to be empty after all unlocks, got %d entries", len(s.execKeyMu))
	}
}
