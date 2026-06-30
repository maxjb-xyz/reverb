package main

import (
	"context"
	"sync/atomic"

	"github.com/maxjb-xyz/reverb/internal/api"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/resolver"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// matcherHolder wraps a resolver.Rematcher so it can live behind an atomic.Pointer
// (atomic.Pointer needs a concrete pointee). The wrapped matcher may be nil when no
// library is configured.
type matcherHolder struct{ m resolver.Rematcher }

// bundleBuilder is the seam serviceReloader builds a fresh ServiceBundle through.
// *wiring.Builder satisfies it; tests inject a stub to drive successive matchers.
type bundleBuilder interface {
	Build(ctx context.Context) (wiring.ServiceBundle, error)
}

// serviceReloader adapts a bundleBuilder to api.ServiceReloader. On each Reload it
// builds a fresh bundle from the current adapter_instance rows, starts the new
// download Manager (the server Stops the previous one after swapping), publishes the
// freshly-built matcher into liveMatcher so the long-lived resolver re-matches
// against the CURRENT adapter, and returns the services as the api interfaces —
// passing typed nils when a concrete service is absent so handlers see a nil
// interface, not a non-nil interface wrapping a nil pointer.
type serviceReloader struct {
	builder bundleBuilder
	// liveMatcher is the shared holder the resolver's provider reads. It is set at
	// boot from the initial bundle and overwritten on every reload, so the resolver
	// singleton (constructed once with matcherProvider) always reaches the live
	// matcher and never a stale captured one. Holds a *matcherHolder whose .m may be
	// nil (no library) — the provider tolerates that.
	liveMatcher atomic.Pointer[matcherHolder]
}

var _ api.ServiceReloader = (*serviceReloader)(nil)

// newServiceReloader builds a reloader over a *wiring.Builder (the production path).
func newServiceReloader(builder *wiring.Builder) *serviceReloader {
	return &serviceReloader{builder: builder}
}

// newServiceReloaderFunc builds a reloader over an arbitrary bundle-builder func.
// Used by tests to drive successive bundles (and thus matchers) without a DB.
func newServiceReloaderFunc(build func(context.Context) (wiring.ServiceBundle, error)) *serviceReloader {
	return &serviceReloader{builder: bundleBuilderFunc(build)}
}

type bundleBuilderFunc func(context.Context) (wiring.ServiceBundle, error)

func (f bundleBuilderFunc) Build(ctx context.Context) (wiring.ServiceBundle, error) {
	return f(ctx)
}

// publishMatcher installs m as the current live matcher. Called once at boot with
// the initial bundle.Matcher and again from Reload after each rebuild. m may be nil.
func (r *serviceReloader) publishMatcher(m resolver.Rematcher) {
	r.liveMatcher.Store(&matcherHolder{m: m})
}

// matcherProvider returns the resolver.Service provider: a func that reads the
// current live matcher on every call, so the resolver follows hot-reloads instead
// of capturing a stale matcher. Returns nil safely before any publish or when no
// library is configured.
func (r *serviceReloader) matcherProvider() func() resolver.Rematcher {
	return func() resolver.Rematcher {
		h := r.liveMatcher.Load()
		if h == nil {
			return nil
		}
		return h.m
	}
}

func (r *serviceReloader) Reload(ctx context.Context) (library.LibraryAdapter, api.Streamer, api.CoverageService, api.DownloadManager, api.SyncService, error) {
	bundle, err := r.builder.Build(ctx)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Publish the freshly-built matcher (may be nil) so the resolver singleton
	// re-matches against the live adapter rather than the one it was wired with.
	r.publishMatcher(bundle.Matcher)

	// LibraryAdapter is itself an interface; a nil bundle.Library is a usable nil
	// interface and the libraryReady guard handles it.
	lib := bundle.Library

	var srch api.Streamer
	if bundle.Aggregator != nil {
		srch = bundle.Aggregator
	}

	// Guard against the non-nil-interface-wrapping-nil-pointer trap: only set the
	// interface when the concrete service is present.
	var cov api.CoverageService
	if bundle.Coverage != nil {
		cov = bundle.Coverage
	}

	var dl api.DownloadManager
	if bundle.Manager != nil {
		bundle.Manager.Start()
		dl = bundle.Manager
	}

	var snc api.SyncService
	if bundle.Sync != nil {
		snc = bundle.Sync
	}

	return lib, srch, cov, dl, snc, nil
}
