// internal/coverage/service.go
package coverage

import (
	"context"
	"encoding/json"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

// DiscoSource is the external source used for discography + resolution.
type DiscoSource interface {
	Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
	GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
	GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error)
}

// LibraryArtist is the library slice the service needs.
type LibraryArtist interface {
	GetArtist(ctx context.Context, id string) (core.Artist, error)
	GetAlbum(ctx context.Context, id string) (core.Album, error)
}

// VersionProvider returns the current monotonic library_version.
type VersionProvider func(ctx context.Context) (int64, error)

// CoverageCache is the persistence slice (satisfied by a thin wrapper over *db.Queries).
type CoverageCache interface {
	GetArtistExternalMap(ctx context.Context, libraryArtistID, source string) (ArtistMapRow, error)
	UpsertArtistExternalMap(ctx context.Context, libraryArtistID, source, externalID string, confidence float64, now int64) error
	GetDiscographyCache(ctx context.Context, source, externalArtistID string) (DiscoRow, error)
	UpsertDiscographyCache(ctx context.Context, source, externalArtistID, albumsJSON string, now int64) error
	GetAlbumCoverage(ctx context.Context, source, externalAlbumID string) (CoverageRow, error)
	UpsertAlbumCoverage(ctx context.Context, source, externalAlbumID, coverageJSON, libraryAlbumID string, libraryVersion int64, now int64) error
}

type ArtistMapRow struct{ ExternalArtistID string; Confidence float64 }
type DiscoRow struct{ AlbumsJSON string }
type CoverageRow struct{ CoverageJSON, LibraryAlbumID string; LibraryVersion int64; Found bool }

type Service struct {
	src     DiscoSource
	match   Matcher
	lib     LibraryArtist
	cache   CoverageCache
	now     func() int64
	version VersionProvider
}

func NewService(src DiscoSource, m Matcher, lib LibraryArtist, cache CoverageCache, now func() int64, version VersionProvider) *Service {
	return &Service{src: src, match: m, lib: lib, cache: cache, now: now, version: version}
}

// ArtistDetail returns the page skeleton. source is "library" or "spotify".
func (s *Service) ArtistDetail(ctx context.Context, source, id string) (core.ArtistDetail, error) {
	extID, libArtistID := "", ""
	det := core.ArtistDetail{Source: source, ID: id}
	if source == "library" {
		libArtistID = id
		art, err := s.lib.GetArtist(ctx, id)
		if err != nil {
			return det, err
		}
		det.Name, det.CoverArtID, det.LibraryArtistID = art.Name, art.CoverArtID, id
		extID, _, _ = ResolveArtist(ctx, s.src, s.lib, s.cache, s.now, id)
	} else {
		extID = id
	}
	if extID == "" {
		// Degrade: show library-owned albums as full.
		det.Resolved = false
		det.Albums = s.libraryAlbumsAsSkeleton(ctx, libArtistID)
		return det, nil
	}
	det.Resolved = true
	det.ExternalArtistID = extID
	albums, err := s.discography(ctx, extID)
	if err != nil {
		return det, err
	}
	det.Albums = Canonicalize(albums)
	if det.Name == "" && len(albums) > 0 {
		det.Name = albums[0].Artist
	}
	return det, nil
}

// discography is cache-first.
func (s *Service) discography(ctx context.Context, extID string) ([]core.ExternalAlbum, error) {
	if row, err := s.cache.GetDiscographyCache(ctx, "spotify", extID); err == nil && row.AlbumsJSON != "" {
		var cached []core.ExternalAlbum
		if json.Unmarshal([]byte(row.AlbumsJSON), &cached) == nil {
			return cached, nil
		}
	}
	albums, err := s.src.GetArtistDiscography(ctx, extID)
	if err != nil {
		return nil, err
	}
	if b, mErr := json.Marshal(albums); mErr == nil {
		_ = s.cache.UpsertDiscographyCache(ctx, "spotify", extID, string(b), s.now())
	}
	return albums, nil
}

func (s *Service) libraryAlbumsAsSkeleton(ctx context.Context, libArtistID string) []core.DiscographyAlbum {
	out := []core.DiscographyAlbum{}
	if libArtistID == "" {
		return out
	}
	art, err := s.lib.GetArtist(ctx, libArtistID)
	if err != nil {
		return out
	}
	for _, al := range art.Albums {
		out = append(out, core.DiscographyAlbum{
			Source: "library", ExternalID: al.ID, Name: al.Name, Year: al.Year,
			Kind: "album", TotalTracks: al.SongCount,
		})
	}
	return out
}

// StreamCoverage emits one AlbumCoverage per canonical album (cache-first).
func (s *Service) StreamCoverage(ctx context.Context, source, id string) <-chan core.AlbumCoverage {
	out := make(chan core.AlbumCoverage)
	go func() {
		defer close(out)
		det, err := s.ArtistDetail(ctx, source, id)
		if err != nil || !det.Resolved {
			return
		}
		for _, da := range det.Albums {
			cov, cErr := s.coverageForAlbum(ctx, da.ExternalID)
			if cErr != nil {
				cov = core.AlbumCoverage{Source: "spotify", ExternalAlbumID: da.ExternalID, State: core.CoverageNone, MissingTracks: []core.ExternalTrackRef{}}
			}
			select {
			case <-ctx.Done():
				return
			case out <- cov:
			}
		}
	}()
	return out
}

func (s *Service) coverageForAlbum(ctx context.Context, extAlbumID string) (core.AlbumCoverage, error) {
	curVer, err := s.version(ctx)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	if row, err := s.cache.GetAlbumCoverage(ctx, "spotify", extAlbumID); err == nil && row.Found && row.LibraryVersion >= curVer {
		var cov core.AlbumCoverage
		if json.Unmarshal([]byte(row.CoverageJSON), &cov) == nil {
			return cov, nil
		}
	}
	full, err := s.src.GetAlbum(ctx, extAlbumID)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	cov, err := RollUp(ctx, s.match, full)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	cov.LibraryAlbumID = s.backfillLibraryAlbumID(ctx, cov)
	if b, mErr := json.Marshal(cov); mErr == nil {
		_ = s.cache.UpsertAlbumCoverage(ctx, "spotify", extAlbumID, string(b), cov.LibraryAlbumID, curVer, s.now())
	}
	return cov, nil
}

// backfillLibraryAlbumID returns the owning library album of the first matched
// track (so a click on a partial/full album opens the local album).
func (s *Service) backfillLibraryAlbumID(ctx context.Context, cov core.AlbumCoverage) string {
	if cov.OwnedCount == 0 {
		return ""
	}
	// We don't carry matched track ids out of RollUp; recompute cheaply via the
	// library album of any owned track is overkill — instead the service stores the
	// linkage when known. For MVP, leave empty unless the rollup is extended; the
	// client falls back to the external album view. (See Task 8 for album-page play.)
	return ""
}

// albumDetailFromExternal builds an AlbumDetail from a full external album by
// matching each track against the library. spotify-source defaults are applied;
// callers that need different metadata (e.g. the library branch) override afterwards.
func (s *Service) albumDetailFromExternal(ctx context.Context, full core.ExternalAlbum) (core.AlbumDetail, error) {
	det := core.AlbumDetail{
		Source: "spotify", ID: full.ExternalID, Name: full.Name, Artist: full.Artist,
		CoverURL: full.CoverURL, Year: full.Year, TotalCount: len(full.Tracks),
	}
	for i, tr := range full.Tracks {
		res, mErr := s.match.Match(ctx, tr)
		if mErr != nil {
			return core.AlbumDetail{}, mErr
		}
		dt := core.AlbumDetailTrack{Title: tr.Title, Artist: tr.Artist, TrackNumber: i + 1, DurationMs: tr.DurationMs}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{ID: res.LibraryTrackID, Title: tr.Title, Artist: tr.Artist, DurationMs: tr.DurationMs}
		} else {
			dt.State = core.CoverageNone
			ref := core.ExternalTrackRef{Source: tr.Source, ExternalID: tr.ExternalID, Title: tr.Title, Artist: tr.Artist, Album: full.Name, ISRC: tr.ISRC, DurationMs: tr.DurationMs}
			dt.ExternalRef = &ref
		}
		det.Tracks = append(det.Tracks, dt)
	}
	return det, nil
}

// resolveExternalAlbum searches the external source for an album matching al by
// normalized title+artist. Returns the external ID and true on success.
func (s *Service) resolveExternalAlbum(ctx context.Context, al core.Album) (string, bool) {
	if s.src == nil {
		return "", false
	}
	cands, err := s.src.Search(ctx, al.Artist+" "+al.Name, core.EntityAlbum)
	if err != nil || len(cands) == 0 {
		return "", false
	}
	normName := matching.Normalize(al.Name)
	normArtist := matching.Normalize(al.Artist)
	for _, c := range cands {
		if matching.Normalize(c.Title) == normName && matching.Normalize(c.Artist) == normArtist {
			return c.ExternalID, true
		}
	}
	return "", false
}

// AlbumDetail returns per-track ownership for an album. source "library" merges
// the full Spotify tracklist (owned + missing) when a match is found; falls back
// to library-only when Spotify isn't configured or no match is found. source
// "spotify" takes the external id directly.
func (s *Service) AlbumDetail(ctx context.Context, source, id string) (core.AlbumDetail, error) {
	if source == "library" {
		al, err := s.lib.GetAlbum(ctx, id)
		if err != nil {
			return core.AlbumDetail{}, err
		}
		extID, ok := s.resolveExternalAlbum(ctx, al)
		if ok {
			full, fErr := s.src.GetAlbum(ctx, extID)
			if fErr != nil {
				return core.AlbumDetail{}, fErr
			}
			det, dErr := s.albumDetailFromExternal(ctx, full)
			if dErr != nil {
				return core.AlbumDetail{}, dErr
			}
			// Override with library-authoritative metadata.
			det.Source = "library"
			det.ID = al.ID
			det.LibraryAlbumID = al.ID
			det.Name = al.Name
			det.Artist = al.Artist
			det.ArtistID = al.ArtistID
			det.CoverArtID = al.CoverArtID
			det.Year = al.Year
			det.CoverURL = ""
			return det, nil
		}
		// Fallback: no external match — return all library tracks as owned.
		det := core.AlbumDetail{
			Source: "library", ID: al.ID, Name: al.Name, Artist: al.Artist, ArtistID: al.ArtistID,
			CoverArtID: al.CoverArtID, Year: al.Year, LibraryAlbumID: al.ID,
			OwnedCount: len(al.Tracks), TotalCount: len(al.Tracks),
		}
		for _, t := range al.Tracks {
			tt := t
			det.Tracks = append(det.Tracks, core.AlbumDetailTrack{
				State: core.CoverageFull, LibraryTrack: &tt, Title: t.Title, Artist: t.Artist,
				TrackNumber: t.TrackNumber, DurationMs: t.DurationMs,
			})
		}
		return det, nil
	}
	full, err := s.src.GetAlbum(ctx, id)
	if err != nil {
		return core.AlbumDetail{}, err
	}
	return s.albumDetailFromExternal(ctx, full)
}
