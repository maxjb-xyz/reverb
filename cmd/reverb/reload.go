package main

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/api"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// serviceReloader adapts a *wiring.Builder to api.ServiceReloader. On each
// Reload it builds a fresh bundle from the current adapter_instance rows, starts
// the new download Manager (the server Stops the previous one after swapping),
// and returns the services as the api interfaces — passing typed nils when a
// concrete service is absent so handlers see a nil interface, not a non-nil
// interface wrapping a nil pointer.
type serviceReloader struct {
	builder *wiring.Builder
}

var _ api.ServiceReloader = (*serviceReloader)(nil)

func (r *serviceReloader) Reload(ctx context.Context) (library.LibraryAdapter, api.Streamer, api.CoverageService, api.DownloadManager, api.SyncService, error) {
	bundle, err := r.builder.Build(ctx)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

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
