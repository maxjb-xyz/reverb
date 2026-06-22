package matching

import (
	"context"
	"database/sql"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// DurationToleranceMs is the max |external-library| duration delta accepted by
// the fuzzy rung. A live cut is rarely within 3s of the studio cut, so duration
// is the disambiguator that prevents cross-version false positives.
const DurationToleranceMs = 3000

// LibrarySearcher is the slice of LibraryAdapter that matching needs.
// *library.Adapter satisfies this structurally — matching does not import library.
type LibrarySearcher interface {
	Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
}

// MatchCacheStore is the slice of *db.Queries that matching needs.
type MatchCacheStore interface {
	GetMatchCache(ctx context.Context, arg db.GetMatchCacheParams) (db.MatchCache, error)
	UpsertMatchCache(ctx context.Context, arg db.UpsertMatchCacheParams) error
}

// VersionProvider returns the current monotonic library_version.
type VersionProvider func(ctx context.Context) (int64, error)

// Service is the MatchingService. It is deterministic and cache-first.
type Service struct {
	lib     LibrarySearcher
	cache   MatchCacheStore
	version VersionProvider
}

// NewService constructs a MatchingService. It satisfies search.Matcher via Match.
func NewService(lib LibrarySearcher, cache MatchCacheStore, version VersionProvider) *Service {
	return &Service{lib: lib, cache: cache, version: version}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Match resolves an external result to a library track via the priority chain
// ISRC → MBID → normalized-fuzzy+duration. Reads/writes match_cache; respects
// library_version for invalidation. Positive AND negative decisions are cached.
func (s *Service) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	curVer, err := s.version(ctx)
	if err != nil {
		return core.MatchResult{}, err
	}

	// 1. Cache-first: serve fresh cached rows without querying the library.
	if s.cache != nil {
		row, cerr := s.cache.GetMatchCache(ctx, db.GetMatchCacheParams{
			Source:     ext.Source,
			ExternalID: ext.ExternalID,
		})
		if cerr == nil {
			if row.LibraryVersion >= curVer {
				return cachedToResult(row), nil
			}
			// Row is stale — fall through to recompute.
		} else if cerr != sql.ErrNoRows {
			return core.MatchResult{}, cerr
		}
	}

	// 2. Type guard: candidate fetch below assumes track-typed externals.
	// Return not_in_library immediately for albums, artists, playlists etc.
	if ext.Type != core.EntityTrack {
		return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone, Confidence: 0}, nil
	}

	// 3. Candidate fetch + priority chain. Try the title query first; if that yields
	// no in-library decision, retry with the artist as a BROADER query. Long titles
	// (notably classical: "Goldberg Variations, BWV 988: Aria — …") frequently return
	// ZERO songs from Navidrome's search3 (its tokenizer matches poorly on long exact
	// strings), so a title-only query left the candidate set empty and the ISRC/fuzzy
	// rungs never had anything to compare — a downloaded classical track stayed
	// permanently unlinked. An artist query returns that artist's catalogue, giving
	// the ISRC rung (and fuzzy) real candidates to resolve against.
	queries := candidateQueries(ext)
	var result core.MatchResult
	for _, q := range queries {
		res, err := s.lib.Search(ctx, q, []core.EntityType{core.EntityTrack})
		if err != nil {
			return core.MatchResult{}, err
		}
		result = s.resolve(ext, res.Tracks)
		if result.Status == core.MatchInLibrary {
			break
		}
	}

	// 5. Write-through cache (positive and negative).
	if s.cache != nil {
		ltid := sql.NullString{}
		if result.Status == core.MatchInLibrary {
			ltid = sql.NullString{String: result.LibraryTrackID, Valid: true}
		}
		if uerr := s.cache.UpsertMatchCache(ctx, db.UpsertMatchCacheParams{
			Source:         ext.Source,
			ExternalID:     ext.ExternalID,
			LibraryTrackID: ltid,
			Method:         string(result.Method),
			Confidence:     result.Confidence,
			Isrc:           ext.ISRC,
			Mbid:           ext.MBID,
			DurationMs:     int64(ext.DurationMs),
			LibraryVersion: curVer,
		}); uerr != nil {
			return core.MatchResult{}, uerr
		}
	}
	return result, nil
}

// candidateQueries returns the library Search queries to try, in order: the title
// (the precise query), then the artist (a broader net that returns the artist's
// catalogue when an exact long-title search returns nothing). Empties are dropped
// and duplicates collapsed, so a track with only an artist still gets one query and
// title==artist doesn't double-search.
func candidateQueries(ext core.ExternalResult) []string {
	var qs []string
	seen := map[string]bool{}
	for _, q := range []string{ext.Title, ext.Artist} {
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		qs = append(qs, q)
	}
	return qs
}

// cachedToResult reconstructs a MatchResult from a cached row.
func cachedToResult(row db.MatchCache) core.MatchResult {
	if row.LibraryTrackID.Valid {
		return core.MatchResult{
			Status:         core.MatchInLibrary,
			LibraryTrackID: row.LibraryTrackID.String,
			Method:         core.MatchMethod(row.Method),
			Confidence:     row.Confidence,
		}
	}
	return core.MatchResult{
		Status:     core.MatchNotInLibrary,
		Method:     core.MatchMethod(row.Method),
		Confidence: row.Confidence,
	}
}

// resolve runs the priority chain against cands and returns the match decision.
// Chain: ISRC exact → MBID exact → normalized fuzzy+duration → not_in_library.
func (s *Service) resolve(ext core.ExternalResult, cands []core.Track) core.MatchResult {
	// ISRC rung: both sides must carry an ISRC.
	if ext.ISRC != "" {
		for _, c := range cands {
			if c.ISRC != "" && c.ISRC == ext.ISRC {
				return core.MatchResult{
					Status:         core.MatchInLibrary,
					LibraryTrackID: c.ID,
					Method:         core.MatchISRC,
					Confidence:     1.0,
				}
			}
		}
	}

	// MBID rung: core.Track has no MBID field in M2; this rung is a structural
	// placeholder that becomes active when a library MBID source is added (P2).
	// It is intentionally a no-op here.

	// Fuzzy rung: normalized title+artist equality, then duration disambiguation.
	nTitle := Normalize(ext.Title)
	nArtist := Normalize(ext.Artist)
	nAlbum := Normalize(ext.Album)

	best := -1
	bestDelta := DurationToleranceMs + 1 // one beyond the threshold
	bestAlbumMatch := false

	for i, c := range cands {
		if Normalize(c.Title) != nTitle || Normalize(c.Artist) != nArtist {
			continue
		}
		// Duration only DISAMBIGUATES; it must not REJECT when the external side
		// has no duration. The post-download re-match carries no DurationMs (the
		// job doesn't store one), so ext.DurationMs is 0 — without this guard every
		// candidate's delta is its full length and nothing ever matches, leaving
		// downloads permanently unlinked (no play / no cover).
		delta := 0
		if ext.DurationMs > 0 {
			delta = absInt(c.DurationMs - ext.DurationMs)
			if delta > DurationToleranceMs {
				continue
			}
		}
		albumMatch := nAlbum != "" && Normalize(c.Album) == nAlbum
		// Prefer smaller delta; on tie, prefer album match.
		better := delta < bestDelta || (delta == bestDelta && albumMatch && !bestAlbumMatch)
		if best == -1 || better {
			best = i
			bestDelta = delta
			bestAlbumMatch = albumMatch
		}
	}

	if best >= 0 {
		// Confidence heuristic: album match boosts to 0.9, otherwise 0.7.
		conf := 0.7
		if bestAlbumMatch {
			conf = 0.9
		}
		return core.MatchResult{
			Status:         core.MatchInLibrary,
			LibraryTrackID: cands[best].ID,
			Method:         core.MatchFuzzy,
			Confidence:     conf,
		}
	}

	return core.MatchResult{
		Status:     core.MatchNotInLibrary,
		Method:     core.MatchNone,
		Confidence: 0,
	}
}
