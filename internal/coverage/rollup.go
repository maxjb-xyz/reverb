// internal/coverage/rollup.go
package coverage

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// Matcher is the slice of *matching.Service the rollup needs.
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

// RollUp computes exact coverage for one album. full iff every track matches.
// LibraryAlbumID is left empty here; the service (Task 6) backfills it from
// library lookups because MatchResult carries only the track id.
func RollUp(ctx context.Context, m Matcher, al core.ExternalAlbum) (core.AlbumCoverage, error) {
	cov := core.AlbumCoverage{
		Source:          al.Source,
		ExternalAlbumID: al.ExternalID,
		TotalCount:      len(al.Tracks),
		MissingTracks:   []core.ExternalTrackRef{},
	}
	for _, tr := range al.Tracks {
		res, err := m.Match(ctx, tr)
		if err != nil {
			return core.AlbumCoverage{}, err
		}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			cov.OwnedCount++
		} else {
			cov.MissingTracks = append(cov.MissingTracks, core.ExternalTrackRef{
				Source:     tr.Source,
				ExternalID: tr.ExternalID,
				Title:      tr.Title,
				Artist:     tr.Artist,
				Album:      al.Name,
				ISRC:       tr.ISRC,
				DurationMs: tr.DurationMs,
			})
		}
	}
	switch {
	case cov.TotalCount > 0 && cov.OwnedCount == cov.TotalCount:
		cov.State = core.CoverageFull
	case cov.OwnedCount == 0:
		cov.State = core.CoverageNone
	default:
		cov.State = core.CoveragePartial
	}
	return cov, nil
}
