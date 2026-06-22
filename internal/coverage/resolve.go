// internal/coverage/resolve.go
package coverage

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

// ResolveArtist maps a library artist to an external artist id, caching the result.
// Returns ("",0,nil) when no confident match (caller degrades to library-only).
func ResolveArtist(ctx context.Context, src DiscoSource, lib LibraryArtist, cache CoverageCache, now func() int64, libraryArtistID string) (string, float64, error) {
	if row, err := cache.GetArtistExternalMap(ctx, libraryArtistID, "spotify"); err == nil && row.ExternalArtistID != "" {
		return row.ExternalArtistID, row.Confidence, nil
	}
	art, err := lib.GetArtist(ctx, libraryArtistID)
	if err != nil {
		return "", 0, err
	}
	cands, err := src.Search(ctx, art.Name, core.EntityArtist)
	if err != nil || len(cands) == 0 {
		return "", 0, err
	}
	want := matching.Normalize(art.Name)
	for _, c := range cands {
		if matching.Normalize(c.Title) == want {
			_ = cache.UpsertArtistExternalMap(ctx, libraryArtistID, "spotify", c.ExternalID, 1.0, now())
			return c.ExternalID, 1.0, nil
		}
	}
	// No candidate's normalized name matches the library artist — accepting Spotify's
	// top result would resolve an absent artist to a WRONG discography. Per the spec
	// ("no confident match → graceful degrade to library-only") we degrade WITHOUT
	// caching a positive, so a later re-resolve can still succeed.
	return "", 0, nil
}
