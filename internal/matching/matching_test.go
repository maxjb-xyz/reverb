package matching

import (
	"context"
	"database/sql"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// fakeLib returns a fixed candidate set for any query (the case's library tracks).
type fakeLib struct{ tracks []core.Track }

func (f fakeLib) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{Tracks: f.tracks}, nil
}

// countingLib wraps a LibrarySearcher and counts how many times Search is
// invoked, so tests can assert cache hits avoid the library query entirely.
type countingLib struct {
	inner LibrarySearcher
	calls int
}

func (c *countingLib) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	c.calls++
	return c.inner.Search(ctx, q, types)
}

// memCache is an in-memory MatchCacheStore.
type memCache struct{ rows map[string]db.MatchCache }

func newMemCache() *memCache { return &memCache{rows: map[string]db.MatchCache{}} }
func key(s, e string) string { return s + "\x1f" + e }
func (m *memCache) GetMatchCache(ctx context.Context, arg db.GetMatchCacheParams) (db.MatchCache, error) {
	r, ok := m.rows[key(arg.Source, arg.ExternalID)]
	if !ok {
		return db.MatchCache{}, sql.ErrNoRows
	}
	return r, nil
}
func (m *memCache) UpsertMatchCache(ctx context.Context, arg db.UpsertMatchCacheParams) error {
	m.rows[key(arg.Source, arg.ExternalID)] = db.MatchCache{
		Source: arg.Source, ExternalID: arg.ExternalID, LibraryTrackID: arg.LibraryTrackID,
		Method: arg.Method, Confidence: arg.Confidence, Isrc: arg.Isrc, Mbid: arg.Mbid,
		DurationMs: arg.DurationMs, LibraryVersion: arg.LibraryVersion,
		ArtistID: arg.ArtistID, AlbumID: arg.AlbumID, CoverArtID: arg.CoverArtID,
	}
	return nil
}

func fixtureToTrack(ft fixtureTrack) core.Track {
	return core.Track{ID: ft.ID, Title: ft.Title, Artist: ft.Artist, Album: ft.Album, DurationMs: ft.DurationMs, ISRC: ft.ISRC}
}
func fixtureToExternal(ft fixtureTrack) core.ExternalResult {
	return core.ExternalResult{
		Source: "spotify", ExternalID: "ext-" + ft.Title, Title: ft.Title, Artist: ft.Artist,
		Album: ft.Album, DurationMs: ft.DurationMs, ISRC: ft.ISRC, MBID: ft.MBID, Type: core.EntityTrack,
	}
}

func TestMatchAgainstFixtures(t *testing.T) {
	for _, c := range loadFixtures(t) {
		t.Run(c.Name, func(t *testing.T) {
			var cands []core.Track
			for _, lt := range c.Library {
				cands = append(cands, fixtureToTrack(lt))
			}
			svc := NewService(fakeLib{tracks: cands}, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
			ext := fixtureToExternal(c.External)

			got, err := svc.Match(context.Background(), ext)
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Status) != c.Expect.Status {
				t.Fatalf("status=%q want %q (result %+v)", got.Status, c.Expect.Status, got)
			}
			if got.LibraryTrackID != c.Expect.LibraryTrackID {
				t.Fatalf("libraryTrackId=%q want %q", got.LibraryTrackID, c.Expect.LibraryTrackID)
			}
			if c.Expect.Method != "" && string(got.Method) != c.Expect.Method {
				t.Fatalf("method=%q want %q", got.Method, c.Expect.Method)
			}
		})
	}
}

func TestMatchZeroDurationStillMatches(t *testing.T) {
	// The post-download re-match carries no DurationMs (0). Duration must only
	// disambiguate, never reject — otherwise a title+artist match is dropped and
	// the download is never linked (no play / no cover).
	cands := []core.Track{{ID: "lib-1", Title: "COMË N GO", Artist: "Yeat", Album: "Dangerous Summer", DurationMs: 180000}}
	svc := NewService(fakeLib{tracks: cands}, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{
		Source: "spotify", ExternalID: "sp-x", Type: core.EntityTrack,
		Title: "COMË N GO", Artist: "Yeat", Album: "Dangerous Summer", DurationMs: 0,
	}
	res, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != core.MatchInLibrary || res.LibraryTrackID != "lib-1" {
		t.Fatalf("zero-duration re-match should link to lib-1, got %+v", res)
	}
}

// TestMatchCompositeArtistAndDurationDrift covers the live-app download-match bug:
// spotDL pulls audio from YouTube (so duration drifts several seconds from Spotify's
// metadata) into Navidrome, which joins composite "Composer/Performer/Ensemble"
// artists while Spotify gives only the primary. The matcher must tolerate BOTH —
// without loosening into cross-version false positives. Cases use the real live data.
func TestMatchCompositeArtistAndDurationDrift(t *testing.T) {
	cases := []struct {
		name       string
		ext        core.ExternalResult
		lib        []core.Track
		wantStatus core.MatchStatus
		wantID     string
		wantMethod core.MatchMethod
	}{
		{
			// Chopin Nocturne: composite artist + EXACT album + 7040ms drift (>3000ms
			// gate). Album corroboration bypasses the duration reject → fuzzy match.
			name: "chopin composite artist + album match + 7040ms drift",
			ext: core.ExternalResult{
				Source: "spotify", ExternalID: "sp-chopin", Type: core.EntityTrack,
				Title:  "Nocturnes, Op. 55: No. 1 in F Minor",
				Artist: "Frédéric Chopin",
				Album:  "Chopin: Nocturnes - Sony Classical Originals",
				DurationMs: 341040, ISRC: "",
			},
			lib: []core.Track{{
				ID:     "lib-chopin",
				Title:  "Nocturnes, Op. 55: No. 1 in F Minor",
				Artist: "Frédéric Chopin/Arthur Rubinstein",
				Album:  "Chopin: Nocturnes - Sony Classical Originals",
				DurationMs: 334000, ISRC: "",
			}},
			wantStatus: core.MatchInLibrary, wantID: "lib-chopin", wantMethod: core.MatchFuzzy,
		},
		{
			// Vivaldi: composite artist, duration WITHIN tolerance (880ms). Matches on
			// the subset-artist rung regardless of the album bypass.
			name: "vivaldi composite artist + in-tolerance 880ms",
			ext: core.ExternalResult{
				Source: "spotify", ExternalID: "sp-vivaldi", Type: core.EntityTrack,
				Title:  "The Four Seasons, Violin Concerto in E Major, RV 269 \"Spring\": I. Allegro",
				Artist: "Antonio Vivaldi",
				Album:  "Vivaldi: The Four Seasons",
				DurationMs: 200000,
			},
			lib: []core.Track{{
				ID:     "lib-vivaldi",
				Title:  "The Four Seasons, Violin Concerto in E Major, RV 269 \"Spring\": I. Allegro",
				Artist: "Antonio Vivaldi/Adrian Chandler/La Serenissima",
				Album:  "Vivaldi: The Four Seasons",
				DurationMs: 200880,
			}},
			wantStatus: core.MatchInLibrary, wantID: "lib-vivaldi", wantMethod: core.MatchFuzzy,
		},
		{
			// Post-download job re-match: the job carries no duration (DurationMs:0).
			// Composite artist + album match → linked via the ext.DurationMs==0 guard.
			name: "post-download re-match (duration 0) + composite artist + album match",
			ext: core.ExternalResult{
				Source: "spotify", ExternalID: "sp-satie", Type: core.EntityTrack,
				Title:  "Gymnopédie No. 1",
				Artist: "Erik Satie",
				Album:  "Satie: Piano Works",
				DurationMs: 0,
			},
			lib: []core.Track{{
				ID:     "lib-satie",
				Title:  "Gymnopédie No. 1",
				Artist: "Erik Satie/Philippe Entremont",
				Album:  "Satie: Piano Works",
				DurationMs: 120000,
			}},
			wantStatus: core.MatchInLibrary, wantID: "lib-satie", wantMethod: core.MatchFuzzy,
		},
		{
			// NEGATIVE: different artist with NO token overlap → must not match, even
			// though title is identical. Protects against composite-subset false wins.
			name: "different artist no token overlap rejected",
			ext: core.ExternalResult{
				Source: "spotify", ExternalID: "sp-bach", Type: core.EntityTrack,
				Title:  "Nocturnes, Op. 55: No. 1 in F Minor",
				Artist: "Johann Sebastian Bach",
				Album:  "Chopin: Nocturnes - Sony Classical Originals",
				DurationMs: 341040,
			},
			lib: []core.Track{{
				ID:     "lib-chopin",
				Title:  "Nocturnes, Op. 55: No. 1 in F Minor",
				Artist: "Frédéric Chopin/Arthur Rubinstein",
				Album:  "Chopin: Nocturnes - Sony Classical Originals",
				DurationMs: 334000,
			}},
			wantStatus: core.MatchNotInLibrary,
		},
		{
			// NEGATIVE: same title+artist but DIFFERENT album and a 60000ms drift with
			// NO album corroboration → the duration gate must still reject, preserving
			// the live-vs-studio cross-version protection.
			name: "same title+artist different album 60000ms drift rejected",
			ext: core.ExternalResult{
				Source: "spotify", ExternalID: "sp-ver", Type: core.EntityTrack,
				Title:  "Falling",
				Artist: "Cinder",
				Album:  "Embers",
				DurationMs: 244000,
			},
			lib: []core.Track{{
				ID:     "lib-live",
				Title:  "Falling",
				Artist: "Cinder",
				Album:  "Live at the Hall",
				DurationMs: 304000,
			}},
			wantStatus: core.MatchNotInLibrary,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(fakeLib{tracks: tc.lib}, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
			got, err := svc.Match(context.Background(), tc.ext)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status=%q want %q (result %+v)", got.Status, tc.wantStatus, got)
			}
			if got.LibraryTrackID != tc.wantID {
				t.Fatalf("libraryTrackId=%q want %q (result %+v)", got.LibraryTrackID, tc.wantID, got)
			}
			if tc.wantMethod != "" && got.Method != tc.wantMethod {
				t.Fatalf("method=%q want %q", got.Method, tc.wantMethod)
			}
		})
	}
}

// TestArtistMatches unit-tests the composite-aware artist comparator directly:
// exact equality, subset both directions, feat markers, and the NEGATIVE no-overlap
// case. Confirms we do NOT split on plain comma or " x "/" vs " (single-name risk).
func TestArtistMatches(t *testing.T) {
	cases := []struct {
		ext, lib string
		want     bool
	}{
		{"Frédéric Chopin", "Frédéric Chopin/Arthur Rubinstein", true},
		{"Antonio Vivaldi", "Antonio Vivaldi/Adrian Chandler/La Serenissima", true},
		{"Erik Satie", "Erik Satie/Philippe Entremont", true},
		{"Frédéric Chopin", "Frédéric Chopin", true},                 // exact
		{"DJ Sol feat. Aluna", "DJ Sol", true},                       // feat marker subset
		{"Nova & Mara", "Nova", true},                                // ampersand subset
		{"Johann Sebastian Bach", "Frédéric Chopin/Arthur Rubinstein", false},
		{"Radiohead", "TLC", false},
		// Plain comma is NOT a separator (single-name protection): "Earth, Wind & Fire"
		// tokenizes via & only → {earth wind, fire}; an unrelated "Fire" is a subset and
		// would match, but a totally unrelated comma'd name does not get falsely split.
		{"Tyler, The Creator", "Drake", false},
		// Slash-in-name guard: "AC/DC" tokenizes to {ac, dc} but both are <3 chars and
		// dropped → {} → a stray external "AC" can NOT subset-match the band "AC/DC".
		{"AC", "AC/DC", false},
		// …but the exact-equality fast path (runs before tokenizing) keeps "AC/DC" itself
		// matching, even though its token set is empty.
		{"AC/DC", "AC/DC", true},
		// Regression: real composite classical credits (all tokens ≥3 chars) still match.
		{"Frédéric Chopin", "Frédéric Chopin/Arthur Rubinstein", true},
	}
	for _, c := range cases {
		if got := artistMatches(c.ext, c.lib); got != c.want {
			t.Errorf("artistMatches(%q, %q)=%v want %v", c.ext, c.lib, got, c.want)
		}
	}
}

// TestArtistTokenSet unit-tests the tokenizer's sub-3-char drop directly: "AC/DC"
// collapses to the empty set (both 2-char tokens dropped) while "Frédéric Chopin/
// Arthur Rubinstein" retains its (≥3-char) composite tokens.
func TestArtistTokenSet(t *testing.T) {
	if got := artistTokenSet("AC/DC"); len(got) != 0 {
		t.Errorf("artistTokenSet(%q)=%v want empty (both tokens <3 chars)", "AC/DC", got)
	}
	got := artistTokenSet("Frédéric Chopin/Arthur Rubinstein")
	if !got["frederic chopin"] || !got["arthur rubinstein"] {
		t.Errorf("artistTokenSet(composite classical)=%v want both ≥3-char tokens present", got)
	}
}

// TestMatchThreadsCandidateMetadata asserts a matched owned track's MatchResult
// carries the candidate's ArtistID/AlbumID/CoverArtID (so the synthesized owned
// LibraryTrack can render clickable links + a real cover), on BOTH a fresh match
// (fuzzy + ISRC rungs) and a cache HIT (reconstructed from the persisted row).
func TestMatchThreadsCandidateMetadata(t *testing.T) {
	cands := []core.Track{{
		ID: "lib-1", Title: "Song", Artist: "A", Album: "X", DurationMs: 200000, ISRC: "USX1",
		ArtistID: "ar-1", AlbumID: "al-1", CoverArtID: "cv-1",
	}}
	cache := newMemCache()
	svc := NewService(fakeLib{tracks: cands}, cache, func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "A", DurationMs: 200000, ISRC: "USX1", Type: core.EntityTrack}

	fresh, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != core.MatchInLibrary {
		t.Fatalf("fresh: expected in_library, got %+v", fresh)
	}
	if fresh.ArtistID != "ar-1" || fresh.AlbumID != "al-1" || fresh.CoverArtID != "cv-1" {
		t.Fatalf("fresh metadata not threaded: %+v", fresh)
	}

	// Cache HIT: the library is now empty, so a non-cached path would lose the ids.
	// The persisted row must reconstruct them.
	svc.lib = fakeLib{tracks: nil}
	hit, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if hit.Status != core.MatchInLibrary {
		t.Fatalf("hit: expected cached positive, got %+v", hit)
	}
	if hit.ArtistID != "ar-1" || hit.AlbumID != "al-1" || hit.CoverArtID != "cv-1" {
		t.Fatalf("cache HIT lost candidate metadata: %+v", hit)
	}
}

func TestMatchCacheFirstAndInvalidation(t *testing.T) {
	cands := []core.Track{{ID: "t1", Title: "Song", Artist: "A", Album: "X", DurationMs: 200000, ISRC: "USX1"}}
	cache := newMemCache()
	version := int64(1)
	svc := NewService(fakeLib{tracks: cands}, cache, func(context.Context) (int64, error) { return version, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "A", DurationMs: 200000, ISRC: "USX1", Type: core.EntityTrack}

	first, _ := svc.Match(context.Background(), ext)
	if first.Status != core.MatchInLibrary || first.Method != core.MatchISRC {
		t.Fatalf("first match: %+v", first)
	}
	// Now the library no longer has the track, but the cached positive (version 1) should still be served.
	svc.lib = fakeLib{tracks: nil}
	cached, _ := svc.Match(context.Background(), ext)
	if cached.Status != core.MatchInLibrary {
		t.Fatalf("expected cached positive, got %+v", cached)
	}
	// Bump library_version → cache stale → re-match against the (now empty) library → negative.
	version = 2
	fresh, _ := svc.Match(context.Background(), ext)
	if fresh.Status != core.MatchNotInLibrary {
		t.Fatalf("expected re-match negative after version bump, got %+v", fresh)
	}
}

// TestMatchCacheAvoidsLibraryQuery proves cache-first EXPLICITLY: a second Match
// for the same external result must NOT re-query the library (call counter stays
// flat), and a library_version bump must force a recompute (counter increments).
func TestMatchCacheAvoidsLibraryQuery(t *testing.T) {
	cands := []core.Track{{ID: "t1", Title: "Song", Artist: "A", Album: "X", DurationMs: 200000, ISRC: "USX1"}}
	lib := &countingLib{inner: fakeLib{tracks: cands}}
	cache := newMemCache()
	version := int64(1)
	svc := NewService(lib, cache, func(context.Context) (int64, error) { return version, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "A", DurationMs: 200000, ISRC: "USX1", Type: core.EntityTrack}

	// First Match computes the decision, which requires one library query.
	first, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != core.MatchInLibrary || first.Method != core.MatchISRC {
		t.Fatalf("first match: %+v", first)
	}
	if lib.calls != 1 {
		t.Fatalf("after first Match: lib.calls=%d want 1", lib.calls)
	}

	// Second Match for the SAME external result must be served from match_cache
	// without touching the library: the counter must NOT increment.
	cached, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Status != core.MatchInLibrary {
		t.Fatalf("expected cached positive, got %+v", cached)
	}
	if lib.calls != 1 {
		t.Fatalf("cache hit queried the library: lib.calls=%d want 1", lib.calls)
	}

	// Bump library_version → cached row is stale → Match must recompute, which
	// requires a fresh library query: the counter must increment.
	version = 2
	fresh, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != core.MatchInLibrary {
		t.Fatalf("expected recompute positive after version bump, got %+v", fresh)
	}
	if lib.calls != 2 {
		t.Fatalf("version bump did not recompute: lib.calls=%d want 2", lib.calls)
	}
}

// TestMatchNonTrackExternalReturnsNotInLibrary verifies that a non-track external
// (e.g. album or artist) returns not_in_library immediately without querying the
// library — the candidate fetch assumes track-typed externals.
func TestMatchNonTrackExternalReturnsNotInLibrary(t *testing.T) {
	lib := &countingLib{inner: fakeLib{tracks: []core.Track{{ID: "t1", Title: "Song", Artist: "A", DurationMs: 200000}}}}
	svc := NewService(lib, nil, func(context.Context) (int64, error) { return 1, nil })

	for _, typ := range []core.EntityType{core.EntityAlbum, core.EntityArtist, core.EntityPlaylist} {
		ext := core.ExternalResult{Source: "spotify", ExternalID: "ext-1", Title: "Song", Artist: "A", DurationMs: 200000, Type: typ}
		got, err := svc.Match(context.Background(), ext)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", typ, err)
		}
		if got.Status != core.MatchNotInLibrary {
			t.Errorf("type %q: status=%q want %q", typ, got.Status, core.MatchNotInLibrary)
		}
	}
	// Library must not have been queried for any of the above non-track types.
	if lib.calls != 0 {
		t.Errorf("library queried %d times for non-track externals, want 0", lib.calls)
	}
}

// queryLib returns candidates keyed by the search query, modelling Navidrome's
// behaviour where a long exact title returns nothing but an artist query returns
// the artist's catalogue.
type queryLib struct {
	byQuery map[string][]core.Track
	queries []string
}

func (q *queryLib) Search(_ context.Context, query string, _ []core.EntityType) (core.SearchResults, error) {
	q.queries = append(q.queries, query)
	return core.SearchResults{Tracks: q.byQuery[query]}, nil
}

// TestMatchFallsBackToArtistQueryForLongTitle is the matching-side regression for
// Bug A: a long classical title returns ZERO songs from a title search, but an
// artist search returns the catalogue, and the ISRC rung then links the track. The
// old title-only matcher would have returned not_in_library, leaving the download
// unlinked forever.
func TestMatchFallsBackToArtistQueryForLongTitle(t *testing.T) {
	const longTitle = "Goldberg Variations, BWV 988: Aria (Remastered 2015)"
	lib := &queryLib{byQuery: map[string][]core.Track{
		// Title query → nothing (Navidrome tokenizer misses the long exact string).
		longTitle: nil,
		// Artist query → the artist's catalogue, including our track (with ISRC).
		"Glenn Gould": {
			{ID: "lib-other", Title: "Some Other Piece", Artist: "Glenn Gould", ISRC: "ZZZ0000000000"},
			{ID: "lib-aria", Title: longTitle, Artist: "Glenn Gould", Album: "Bach: Goldberg Variations", ISRC: "USABC1234567"},
		},
	}}
	svc := NewService(lib, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{
		Source: "spotify", ExternalID: "sp-aria", Type: core.EntityTrack,
		Title: longTitle, Artist: "Glenn Gould", ISRC: "USABC1234567",
	}

	res, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != core.MatchInLibrary || res.LibraryTrackID != "lib-aria" {
		t.Fatalf("expected ISRC match to lib-aria via artist fallback, got %+v", res)
	}
	if res.Method != core.MatchISRC {
		t.Fatalf("expected ISRC method, got %q", res.Method)
	}
	// It must have tried the title query first, then fallen back to the artist query.
	if len(lib.queries) != 2 || lib.queries[0] != longTitle || lib.queries[1] != "Glenn Gould" {
		t.Fatalf("expected [title, artist] query order, got %v", lib.queries)
	}
}

func TestMatchNegativeIsCached(t *testing.T) {
	cache := newMemCache()
	svc := NewService(fakeLib{tracks: nil}, cache, func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp9", Title: "Nope", Artist: "Z", DurationMs: 100000, Type: core.EntityTrack}
	if _, err := svc.Match(context.Background(), ext); err != nil {
		t.Fatal(err)
	}
	row, err := cache.GetMatchCache(context.Background(), db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp9"})
	if err != nil {
		t.Fatalf("negative match not cached: %v", err)
	}
	if row.LibraryTrackID.Valid {
		t.Fatalf("cached negative should have NULL library_track_id: %+v", row)
	}
}

// titleLib returns a single-track candidate set keyed by the search query so that
// a query for "SongA" returns only SongA and a query for "SongB" returns only SongB.
// This gives the fuzzy rung the right candidate per entity.
type titleLib struct {
	tracks map[string]core.Track // title → track
}

func (tl titleLib) Search(_ context.Context, q string, _ []core.EntityType) (core.SearchResults, error) {
	if t, ok := tl.tracks[q]; ok {
		return core.SearchResults{Tracks: []core.Track{t}}, nil
	}
	return core.SearchResults{}, nil
}

// TestMatch_EmptyExternalDoesNotCollideInCache proves that two distinct pure-library
// entities (Source="", ExternalID="") resolved via Match do NOT share a match_cache
// row. Before the fix, both calls wrote to key ("","") so the second call got the
// first entity's result — a silent cross-entity collision.
func TestMatch_EmptyExternalDoesNotCollideInCache(t *testing.T) {
	lib := titleLib{tracks: map[string]core.Track{
		"SongA": {ID: "nav-SongA", Title: "SongA", Artist: "Artist", Album: "Album", DurationMs: 200000},
		"SongB": {ID: "nav-SongB", Title: "SongB", Artist: "Artist", Album: "Album", DurationMs: 200000},
	}}
	cache := newMemCache()
	svc := NewService(lib, cache, func(context.Context) (int64, error) { return 1, nil })

	extA := core.ExternalResult{Source: "", ExternalID: "", Title: "SongA", Artist: "Artist", Album: "Album", DurationMs: 200000, Type: core.EntityTrack}
	extB := core.ExternalResult{Source: "", ExternalID: "", Title: "SongB", Artist: "Artist", Album: "Album", DurationMs: 200000, Type: core.EntityTrack}

	ra, err := svc.Match(context.Background(), extA)
	if err != nil {
		t.Fatalf("Match(SongA) error: %v", err)
	}
	rb, err := svc.Match(context.Background(), extB)
	if err != nil {
		t.Fatalf("Match(SongB) error: %v", err)
	}

	if ra.LibraryTrackID != "nav-SongA" {
		t.Fatalf("SongA: want nav-SongA, got %q", ra.LibraryTrackID)
	}
	if rb.LibraryTrackID != "nav-SongB" {
		t.Fatalf("SongB: want nav-SongB, got %q (collision with SongA?)", rb.LibraryTrackID)
	}
	if ra.LibraryTrackID == rb.LibraryTrackID {
		t.Fatalf("pure-library entities collide in match_cache: both got %q", ra.LibraryTrackID)
	}

	// Guard: empty-external matches must NOT write to match_cache (cache must be empty).
	_, errA := cache.GetMatchCache(context.Background(), db.GetMatchCacheParams{Source: "", ExternalID: ""})
	if errA == nil {
		t.Fatal("match_cache must not have a row for empty (source, external_id)")
	}
}

// TestMatch_EmptyExternalDoesNotWriteMatchCache asserts that when Source="" and
// ExternalID="", Match computes the result fresh each time and never writes a
// match_cache row — so the cache cannot be stale or collide.
func TestMatch_EmptyExternalDoesNotWriteMatchCache(t *testing.T) {
	lib := titleLib{tracks: map[string]core.Track{
		"Solo": {ID: "nav-solo", Title: "Solo", Artist: "Art", DurationMs: 180000},
	}}
	cache := newMemCache()
	svc := NewService(lib, cache, func(context.Context) (int64, error) { return 1, nil })

	ext := core.ExternalResult{Source: "", ExternalID: "", Title: "Solo", Artist: "Art", DurationMs: 180000, Type: core.EntityTrack}
	res, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != core.MatchInLibrary || res.LibraryTrackID != "nav-solo" {
		t.Fatalf("expected match to nav-solo, got %+v", res)
	}

	// No cache row must have been written.
	if len(cache.rows) != 0 {
		t.Fatalf("match_cache must be empty for empty-external match, got %d rows: %v", len(cache.rows), cache.rows)
	}
}
