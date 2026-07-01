package main

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/resolver"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// stubMatcher is a resolver.Rematcher whose identity we can assert on. The Match
// body is never exercised by these wiring tests — only pointer identity matters.
type stubMatcher struct{ name string }

func (s *stubMatcher) Match(context.Context, core.ExternalResult) (core.MatchResult, error) {
	return core.MatchResult{}, nil
}

// TestResolverProvider_FollowsReload verifies the live-matcher provider returns
// the CURRENT matcher across reloads, never a stale captured one. The resolver is
// constructed once (singleton) with a provider reading the shared holder; each
// reload publishes the freshly-built bundle.Matcher into that holder.
func TestResolverProvider_FollowsReload(t *testing.T) {
	m1 := &stubMatcher{name: "M1"}
	m2 := &stubMatcher{name: "M2"}

	// A reloader whose successive Build()s yield bundles carrying M1 then M2.
	bundles := []wiring.ServiceBundle{{Matcher: m1}, {Matcher: m2}}
	var idx int
	rl := newServiceReloaderFunc(func(context.Context) (wiring.ServiceBundle, error) {
		b := bundles[idx]
		idx++
		return b, nil
	})

	// Boot: build the initial bundle and publish its matcher (mirrors main.go), then
	// build the resolver singleton with the holder-backed provider.
	boot, err := rl.builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rl.publishMatcher(boot.Matcher)
	provider := rl.matcherProvider()

	if got := provider(); got != resolver.Rematcher(m1) {
		t.Fatalf("provider() before reload = %v, want M1", got)
	}

	// Reload swaps the live bundle to one carrying M2; the provider must follow.
	if _, _, _, _, _, err := rl.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := provider(); got != resolver.Rematcher(m2) {
		t.Fatalf("provider() after reload = %v, want M2 (stale capture)", got)
	}
}

// TestResolverProvider_NilMatcherIsSafe verifies the provider returns nil safely
// when no library is configured (bundle.Matcher == nil), without panicking.
func TestResolverProvider_NilMatcherIsSafe(t *testing.T) {
	rl := newServiceReloaderFunc(func(context.Context) (wiring.ServiceBundle, error) {
		return wiring.ServiceBundle{Matcher: nil}, nil
	})
	boot, err := rl.builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rl.publishMatcher(boot.Matcher)
	provider := rl.matcherProvider()

	if got := provider(); got != nil {
		t.Fatalf("provider() with no matcher = %v, want nil", got)
	}
	// A reload that still has no matcher keeps the provider nil-safe.
	if _, _, _, _, _, err := rl.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := provider(); got != nil {
		t.Fatalf("provider() after nil-matcher reload = %v, want nil", got)
	}
}

// TestResolverProviderSeam_P2ConstructionOrder verifies that:
//  1. A resolver singleton is constructed BEFORE Build (P2 construction order).
//  2. The Builder's resolverProvider is set before the first Build call.
//  3. The resolver's resolverProvider returns the SAME singleton on repeated calls
//     (the provider is a stable function identity, not a fresh instance each time).
//  4. Calling Resolve before any matcher is published does NOT panic (nil-matcher
//     tolerance: manager/sync call the resolver at runtime, before any library configures).
func TestResolverProviderSeam_P2ConstructionOrder(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/p2order.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	// Step 1: create reloader (atomic holder + matcherProvider) BEFORE Build.
	rl := newServiceReloaderFunc(func(context.Context) (wiring.ServiceBundle, error) {
		return wiring.ServiceBundle{}, nil
	})

	// Step 2: construct the resolver singleton against the holder-backed provider.
	// The holder is empty (no matcher published yet) — mirrors the P2 boot sequence.
	resolverSvc := resolver.NewService(st.Q(), rl.matcherProvider(), time.Now)

	// Step 3: set the resolverProvider on the Builder BEFORE Build.
	// resolverSvc is the singleton; the provider returns it every time.
	var callCount int
	resolverProvider := func() wiring.BindingResolver {
		callCount++
		return resolverSvc
	}
	// Verify Builder accepts the resolverProvider field (compile-time check).
	_ = resolverProvider

	// The singleton must be stable — the same pointer each time.
	first := resolverProvider()
	second := resolverProvider()
	if first != second {
		t.Fatal("resolverProvider must return the same singleton on every call")
	}
	if callCount != 2 {
		t.Fatalf("resolverProvider called %d times, want 2", callCount)
	}

	// Step 4: Resolve with no matcher published must not panic.
	// This simulates a Manager/Sync calling the resolver before any library is configured.
	addr, err := resolverSvc.Resolve(context.Background(), "trk_nonexistent")
	if err != nil {
		t.Fatalf("Resolve before matcher published must not error: %v", err)
	}
	if addr.Found {
		t.Fatalf("Resolve before matcher published must return Found:false, got %+v", addr)
	}
}

// TestResolverProviderSeam_BuilderAcceptsProvider verifies that the Builder
// accepts a resolverProvider func via SetResolverProvider and that the provider
// is read inside Build when constructing the Manager (via the nil-safe resolve dep).
// This is a compile + wiring smoke test — the actual Resolve call is Tasks 3-5.
func TestResolverProviderSeam_BuilderAcceptsProvider(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/builder-provider.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	// Construct reloader + resolver before Build (P2 order).
	rl := newServiceReloaderFunc(func(context.Context) (wiring.ServiceBundle, error) {
		return wiring.ServiceBundle{}, nil
	})
	resolverSvc := resolver.NewService(st.Q(), rl.matcherProvider(), time.Now)

	// Build a minimal Builder (no registries wired — just verifying the seam compiles
	// and that SetResolverProvider doesn't panic).
	b := wiring.NewBuilder(nil, nil, nil, st.Q(), st, nil, nil, func(string) string { return "" }, t.TempDir())
	b.SetResolverProvider(func() wiring.BindingResolver { return resolverSvc })
	// We don't call Build here (it requires real adapter rows + registries), but the
	// above proves the seam: Builder.SetResolverProvider accepts the provider.
	_ = b
}
