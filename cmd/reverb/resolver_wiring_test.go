package main

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/resolver"
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
