package resolver

import (
	"context"
	"testing"
)

// TestBumpEpochHelperAndServiceProduceSameResult verifies that the package-level
// BumpEpoch helper and Service.BumpEpoch use the same key format and produce the
// same epoch values. If the refactor drifts (e.g. the key constant changes in
// one place but not the other), this test catches it immediately.
func TestBumpEpochHelperAndServiceProduceSameResult(t *testing.T) {
	// Two fresh stores so the two code-paths start from identical state.
	stA := openStore(t)
	stB := openStore(t)
	ctx := context.Background()
	const identity = "builtin"

	// Path A: package-level helper called directly (the wiring path).
	if err := BumpEpoch(ctx, stA.Q(), identity); err != nil {
		t.Fatalf("BumpEpoch helper: %v", err)
	}

	// Path B: Service.BumpEpoch (the existing service method, refactored to delegate).
	svc := NewService(stB.Q(), func() Rematcher { return nil }, nil)
	if err := svc.BumpEpoch(ctx, identity); err != nil {
		t.Fatalf("Service.BumpEpoch: %v", err)
	}

	// Both should produce epoch=2 (starting from absent → default 1, then +1).
	// Read via the service's internal epoch() so we use the exact same read path
	// the resolver uses at runtime. Two fresh services, one per store.
	svcA := NewService(stA.Q(), func() Rematcher { return nil }, nil)
	svcB := NewService(stB.Q(), func() Rematcher { return nil }, nil)

	epochA := svcA.epoch(ctx, identity)
	epochB := svcB.epoch(ctx, identity)

	if epochA != epochB {
		t.Fatalf("epoch mismatch: BumpEpoch helper produced %d, Service.BumpEpoch produced %d", epochA, epochB)
	}
	if epochA != 2 {
		t.Fatalf("expected epoch=2 after one bump from absent; got %d", epochA)
	}
}
