// internal/coverage/service.go
package coverage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

// DiscoSource is the external source used for discography + resolution.
type DiscoSource interface {
	Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
	GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
	GetArtist(ctx context.Context, externalID string) (core.ExternalArtist, error)
	GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error)
}

// LibraryArtist is the library slice the service needs.
type LibraryArtist interface {
	GetArtist(ctx context.Context, id string) (core.Artist, error)
	GetAlbum(ctx context.Context, id string) (core.Album, error)
	Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
}

// VersionProvider returns the current monotonic library_version.
type VersionProvider func(ctx context.Context) (int64, error)

// CoverageCache is the persistence slice (satisfied by a thin wrapper over *db.Queries).
type CoverageCache interface {
	GetArtistExternalMap(ctx context.Context, libraryArtistID, source string) (ArtistMapRow, error)
	UpsertArtistExternalMap(ctx context.Context, libraryArtistID, source, externalID string, confidence float64, now int64) error
	GetAlbumExternalMap(ctx context.Context, libraryAlbumID, source string) (AlbumMapRow, error)
	UpsertAlbumExternalMap(ctx context.Context, libraryAlbumID, source, externalID string, confidence float64, now int64) error
	GetDiscographyCache(ctx context.Context, source, externalArtistID string) (DiscoRow, error)
	UpsertDiscographyCache(ctx context.Context, source, externalArtistID, albumsJSON string, now int64) error
	GetAlbumCoverage(ctx context.Context, source, externalAlbumID string) (CoverageRow, error)
	UpsertAlbumCoverage(ctx context.Context, source, externalAlbumID, coverageJSON, libraryAlbumID string, libraryVersion int64, now int64) error
	// GetLibraryAlbumIDByExternal returns the library album id for a known
	// (source, externalAlbumID) pair from the coverage cache, or "" when absent.
	GetLibraryAlbumIDByExternal(ctx context.Context, source, externalAlbumID string) string
}

type ArtistMapRow struct {
	ExternalArtistID string
	Confidence       float64
}
type AlbumMapRow struct {
	ExternalAlbumID string
	Confidence      float64
}
type DiscoRow struct{ AlbumsJSON string }
type CoverageRow struct {
	CoverageJSON, LibraryAlbumID string
	LibraryVersion               int64
	Found                        bool
}

type Service struct {
	sources       map[string]DiscoSource
	defaultSource string
	match         Matcher
	lib           LibraryArtist
	cache         CoverageCache
	now           func() int64
	version       VersionProvider
}

func NewService(src DiscoSource, m Matcher, lib LibraryArtist, cache CoverageCache, now func() int64, version VersionProvider) *Service {
	return NewMultiService(map[string]DiscoSource{"spotify": src}, "spotify", m, lib, cache, now, version)
}

// NewMultiService builds coverage against every configured source that supports
// artist profiles, discographies, and album detail. defaultSource is used only
// when resolving legacy library-only routes.
func NewMultiService(sources map[string]DiscoSource, defaultSource string, m Matcher, lib LibraryArtist, cache CoverageCache, now func() int64, version VersionProvider) *Service {
	return &Service{sources: sources, defaultSource: defaultSource, match: m, lib: lib, cache: cache, now: now, version: version}
}

func (s *Service) source(name string) (DiscoSource, error) {
	src := s.sources[name]
	if src == nil {
		return nil, fmt.Errorf("source %q is not configured for coverage", name)
	}
	return src, nil
}

// ArtistProfile returns a lightweight artist profile (name + image) for the
// given source and id. It makes exactly one upstream call — no discography,
// no library search. Callers use this for the now-playing "About the artist" card.
func (s *Service) ArtistProfile(ctx context.Context, source, id string) (core.ExternalArtist, error) {
	switch source {
	case "library":
		art, err := s.lib.GetArtist(ctx, id)
		if err != nil {
			return core.ExternalArtist{}, err
		}
		return core.ExternalArtist{
			Source:     "library",
			ExternalID: id,
			Name:       art.Name,
			CoverArtID: art.CoverArtID,
		}, nil
	}
	src, err := s.source(source)
	if err != nil {
		return core.ExternalArtist{}, err
	}
	prof, err := src.GetArtist(ctx, id)
	if err != nil {
		return core.ExternalArtist{}, err
	}
	prof.Source = source
	prof.ExternalID = id
	return prof, nil
}

// ArtistDetail returns the page skeleton for a library or configured external source.
func (s *Service) ArtistDetail(ctx context.Context, source, id string) (core.ArtistDetail, error) {
	extID, libArtistID := "", ""
	externalSource := source
	det := core.ArtistDetail{Source: source, ID: id}
	if source == "library" {
		externalSource = s.defaultSource
		src, srcErr := s.source(externalSource)
		if srcErr != nil {
			return det, srcErr
		}
		libArtistID = id
		art, err := s.lib.GetArtist(ctx, id)
		if err != nil {
			return det, err
		}
		det.Name, det.CoverArtID, det.LibraryArtistID = art.Name, art.CoverArtID, id
		extID, _, _ = ResolveArtist(ctx, externalSource, src, s.lib, s.cache, s.now, id)
	} else {
		extID = id
	}
	if extID == "" {
		// Degrade: show library-owned albums as full.
		det.Resolved = false
		det.Albums = s.libraryAlbumsAsSkeleton(ctx, libArtistID)
		det.LibraryAlbums = s.libraryAlbumsByArtistName(ctx, det.Name)
		return det, nil
	}
	src, err := s.source(externalSource)
	if err != nil {
		return det, err
	}
	det.Resolved = true
	det.ExternalArtistID = extID
	// Fetch the artist's real profile (name + image) from the external source.
	if prof, pErr := src.GetArtist(ctx, extID); pErr == nil {
		if det.Name == "" {
			det.Name = prof.Name
		}
		det.CoverURL = prof.CoverURL
	}
	albums, err := s.discography(ctx, externalSource, extID)
	if err != nil {
		return det, err
	}
	det.Albums = Canonicalize(albums)
	// Last-resort fallback: if GetArtist failed or returned no name, derive from discography.
	if det.Name == "" && len(albums) > 0 {
		det.Name = albums[0].Artist
	}
	// Backfill LibraryAlbumID for albums already mapped in the coverage cache.
	for i, da := range det.Albums {
		if libID := s.cache.GetLibraryAlbumIDByExternal(ctx, externalSource, da.ExternalID); libID != "" {
			det.Albums[i].LibraryAlbumID = libID
		}
	}
	// Populate locally-owned albums by searching the library by artist name.
	det.LibraryAlbums = s.libraryAlbumsByArtistName(ctx, det.Name)
	return det, nil
}

// discography is cache-first.
func (s *Service) discography(ctx context.Context, source, extID string) ([]core.ExternalAlbum, error) {
	if row, err := s.cache.GetDiscographyCache(ctx, source, extID); err == nil && row.AlbumsJSON != "" {
		var cached []core.ExternalAlbum
		if json.Unmarshal([]byte(row.AlbumsJSON), &cached) == nil {
			return cached, nil
		}
	}
	src, err := s.source(source)
	if err != nil {
		return nil, err
	}
	albums, err := src.GetArtistDiscography(ctx, extID)
	if err != nil {
		return nil, err
	}
	if b, mErr := json.Marshal(albums); mErr == nil {
		_ = s.cache.UpsertDiscographyCache(ctx, source, extID, string(b), s.now())
	}
	return albums, nil
}

// libraryAlbumsByArtistName searches the library for albums whose artist name
// matches artistName and returns them as deduped DiscographyAlbum entries with
// source "library". Errors are logged and an empty slice is returned (graceful degrade).
func (s *Service) libraryAlbumsByArtistName(ctx context.Context, artistName string) []core.DiscographyAlbum {
	if artistName == "" {
		return []core.DiscographyAlbum{}
	}
	res, err := s.lib.Search(ctx, artistName, []core.EntityType{core.EntityAlbum})
	if err != nil {
		log.Printf("coverage: libraryAlbumsByArtistName search error for %q: %v", artistName, err)
		return []core.DiscographyAlbum{}
	}
	normArtist := matching.Normalize(artistName)
	seen := map[string]struct{}{}
	out := []core.DiscographyAlbum{}
	for _, al := range res.Albums {
		// Only include albums whose artist matches this artist (normalized).
		if matching.Normalize(al.Artist) != normArtist {
			continue
		}
		key := fmt.Sprintf("%s|%s", matching.Normalize(al.Name), al.ID)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, core.DiscographyAlbum{
			Source:         "library",
			ExternalID:     al.ID,
			Name:           al.Name,
			Year:           al.Year,
			Kind:           "album",
			TotalTracks:    al.SongCount,
			LibraryAlbumID: al.ID,
		})
	}
	return out
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
			cov, cErr := s.coverageForAlbum(ctx, da.Source, da.ExternalID)
			if cErr != nil {
				cov = core.AlbumCoverage{Source: da.Source, ExternalAlbumID: da.ExternalID, State: core.CoverageNone, MissingTracks: []core.ExternalTrackRef{}}
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

func (s *Service) coverageForAlbum(ctx context.Context, source, extAlbumID string) (core.AlbumCoverage, error) {
	curVer, err := s.version(ctx)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	if row, err := s.cache.GetAlbumCoverage(ctx, source, extAlbumID); err == nil && row.Found && row.LibraryVersion >= curVer {
		var cov core.AlbumCoverage
		if json.Unmarshal([]byte(row.CoverageJSON), &cov) == nil {
			return cov, nil
		}
	}
	src, err := s.source(source)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	full, err := src.GetAlbum(ctx, extAlbumID)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	cov, err := RollUp(ctx, s.match, full)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	cov.LibraryAlbumID = s.backfillLibraryAlbumID(ctx, cov)
	if b, mErr := json.Marshal(cov); mErr == nil {
		_ = s.cache.UpsertAlbumCoverage(ctx, source, extAlbumID, string(b), cov.LibraryAlbumID, curVer, s.now())
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
// matching each track against the library. Source metadata is preserved;
// callers that need different metadata (e.g. the library branch) override afterwards.
func (s *Service) albumDetailFromExternal(ctx context.Context, full core.ExternalAlbum) (core.AlbumDetail, error) {
	det := core.AlbumDetail{
		Source: full.Source, ID: full.ExternalID, Name: full.Name, Artist: full.Artist,
		CoverURL: full.CoverURL, Year: full.Year, TotalCount: len(full.Tracks),
	}
	for i, tr := range full.Tracks {
		res, mErr := s.match.Match(ctx, tr)
		if mErr != nil {
			return core.AlbumDetail{}, mErr
		}
		dt := core.AlbumDetailTrack{Title: tr.Title, Artist: tr.Artist, Album: full.Name, TrackNumber: i + 1, DurationMs: tr.DurationMs, CoverURL: tr.CoverURL,
			ArtistExternalID: tr.ArtistExternalID, AlbumExternalID: tr.AlbumExternalID}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{ID: res.LibraryTrackID, Title: tr.Title, Artist: tr.Artist, DurationMs: tr.DurationMs, ArtistID: res.ArtistID, AlbumID: res.AlbumID, CoverArtID: res.CoverArtID}
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
// normalized title+artist. Checks the album_external_map cache first; on a miss,
// performs the selected source's live Search and caches a successful resolution. Returns the
// external ID and true on success.
func (s *Service) resolveExternalAlbum(ctx context.Context, source string, al core.Album) (string, bool) {
	src, err := s.source(source)
	if err != nil {
		return "", false
	}
	// Cache-first: if we already resolved this library album, skip the Search.
	if row, err := s.cache.GetAlbumExternalMap(ctx, al.ID, source); err == nil && row.ExternalAlbumID != "" {
		return row.ExternalAlbumID, true
	}
	cands, err := src.Search(ctx, al.Artist+" "+al.Name, core.EntityAlbum)
	if err != nil || len(cands) == 0 {
		return "", false
	}
	normName := matching.Normalize(al.Name)
	normArtist := matching.Normalize(al.Artist)
	for _, c := range cands {
		if matching.Normalize(c.Title) == normName && matching.Normalize(c.Artist) == normArtist {
			_ = s.cache.UpsertAlbumExternalMap(ctx, al.ID, source, c.ExternalID, 1.0, s.now())
			return c.ExternalID, true
		}
	}
	// No confident match — do not cache a negative resolution, same as artist.
	return "", false
}

// AlbumDetail returns per-track ownership for an album. Library routes use the
// default configured source; external routes use their explicit source.
func (s *Service) AlbumDetail(ctx context.Context, source, id string) (core.AlbumDetail, error) {
	if source == "library" {
		al, err := s.lib.GetAlbum(ctx, id)
		if err != nil {
			return core.AlbumDetail{}, err
		}
		extID, ok := s.resolveExternalAlbum(ctx, s.defaultSource, al)
		if ok {
			src, srcErr := s.source(s.defaultSource)
			if srcErr != nil {
				return core.AlbumDetail{}, srcErr
			}
			full, fErr := src.GetAlbum(ctx, extID)
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
	src, err := s.source(source)
	if err != nil {
		return core.AlbumDetail{}, err
	}
	full, err := src.GetAlbum(ctx, id)
	if err != nil {
		return core.AlbumDetail{}, err
	}
	return s.albumDetailFromExternal(ctx, full)
}
