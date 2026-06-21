// Package library defines the LibraryAdapter contract and a conformance suite
// every adapter must pass. Library data is never persisted by Reverb — it always
// flows through an adapter, so a future standalone (folder-scan) adapter is a
// drop-in replacement.
package library

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

type LibraryAdapter interface {
	registry.Plugin

	Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
	GetArtist(ctx context.Context, id string) (core.Artist, error)
	GetAlbum(ctx context.Context, id string) (core.Album, error)
	GetPlaylists(ctx context.Context) ([]core.Playlist, error)

	// CreatePlaylist creates an empty (or single-seed) playlist and returns it.
	CreatePlaylist(ctx context.Context, name string) (core.Playlist, error)
	// AddTracksToPlaylist appends the given library track IDs to a playlist.
	AddTracksToPlaylist(ctx context.Context, playlistID string, trackIDs []string) error

	// Stream forwards rangeHeader (the browser's inbound Range, may be "")
	// to the upstream source and returns the upstream response for proxying.
	Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error)
	CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error)

	// StartScan / ScanStatus are library-maintenance operations modeled on
	// Subsonic/Navidrome. A future folder-scan adapter (P3) owns scanning
	// itself and MAY implement these as no-ops — see RunConformance.
	StartScan(ctx context.Context) error
	ScanStatus(ctx context.Context) (core.ScanStatus, error)
}
