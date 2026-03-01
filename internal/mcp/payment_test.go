package mcp

import (
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

	s := NewServer(cfg, nil)
	if s.x402Enabled {
		t.Fatal("expected x402 gating to remain disabled for incomplete mode=on config")
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

func TestLockForExecutionKey_SerializesAndCleansUpWithRefCounts(t *testing.T) {
	s := &Server{execKeyMu: make(map[string]*keyMutex)}
	key := "same-signature:same-params"

	unlockA := s.lockForExecutionKey(key)

	bAcquired := make(chan struct{})
	bRelease := make(chan struct{})
	startedBlocking := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// signal that goroutine B is about to attempt the keyed lock
		close(startedBlocking)
		unlockB := s.lockForExecutionKey(key)
		close(bAcquired)
		<-bRelease
		unlockB()
	}()

	// wait for B to reach the lock attempt and thus increment ref
	<-startedBlocking
	s.execMu.Lock()
	km, ok := s.execKeyMu[key]
	if !ok || km.ref != 2 {
		s.execMu.Unlock()
		ref := -1
		if km != nil {
			ref = km.ref
		}
		t.Fatalf("expected key mutex ref=2 while B waits, got ok=%v ref=%d", ok, ref)
	}
	s.execMu.Unlock()

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

	// While B holds the lock, C must still be blocked on the same keyed mutex.
	select {
	case <-cAcquired:
		t.Fatal("C acquired lock while B still held it; keyed exclusion broken")
	case <-time.After(50 * time.Millisecond):
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
