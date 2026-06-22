package main

import (
	"context"
	"log"

	"github.com/maxjb-xyz/reverb/internal/api"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// coverageInvalidator is the slice of *coverage.Service the invalidation loop needs.
type coverageInvalidator interface {
	InvalidateLibraryAlbum(ctx context.Context, libraryAlbumID string) error
}

// startCoverageInvalidation subscribes to library.updated and drops cached album
// coverage for each affected album id, so the next artist/album page recomputes
// ownership after a scan-driven re-match. The invalidation hits the DB directly
// (via the shared *db.Queries), so it stays correct even after an adapter reload
// swaps the live coverage service. It runs until the bus subscription is closed.
func startCoverageInvalidation(ctx context.Context, bus *events.Bus, cov coverageInvalidator) {
	ch, _ := bus.Subscribe(download.TopicLibraryUpdate)
	go func() {
		for ev := range ch {
			upd, ok := ev.Payload.(core.LibraryUpdatedEvent)
			if !ok {
				continue
			}
			for _, albumID := range upd.AlbumIDs {
				if albumID == "" {
					continue
				}
				if err := cov.InvalidateLibraryAlbum(ctx, albumID); err != nil {
					log.Printf("coverage invalidation for album %q failed: %v", albumID, err)
				}
			}
		}
	}()
}

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

func (r *serviceReloader) Reload(ctx context.Context) (library.LibraryAdapter, api.Streamer, api.CoverageService, api.DownloadManager, error) {
	bundle, err := r.builder.Build(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
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

	return lib, srch, cov, dl, nil
}
