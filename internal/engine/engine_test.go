package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/maxjb-xyz/reverb/internal/analysis"
	"math/rand"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

var now = time.Unix(1_800_000_000, 0)

type fakeTaste struct{ in taste.Inputs }

func (f *fakeTaste) Profile(context.Context, string) (*taste.Profile, error) {
	return taste.Build("u1", now, f.in), nil
}

type fakeSimilar map[string][]libindex.SimilarArtist

func (f fakeSimilar) Similar(_ context.Context, key string) ([]libindex.SimilarArtist, error) {
	return f[key], nil
}

type fakeTop struct {
	search.SearchSource
	calls int
}

func (f *fakeTop) Name() string { return "deezer" }
func (f *fakeTop) ArtistTopTracks(_ context.Context, id string, _ int) ([]core.ExternalResult, error) {
	f.calls++
	return []core.ExternalResult{{Source: "deezer", ExternalID: "t-" + id, Title: "Hit of " + id, Artist: id}}, nil
}

type env struct {
	q     *db.Queries
	taste *fakeTaste
	sim   fakeSimilar
	top   *fakeTop
	eng   *Engine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/engine.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	e := &env{q: st.Q(), taste: &fakeTaste{}, sim: fakeSimilar{}, top: &fakeTop{}}
	e.eng = New(e.q, e.taste, e.sim, func() []search.ArtistTopTracksProvider { return []search.ArtistTopTracksProvider{e.top} },
		func() time.Time { return now }, rand.New(rand.NewSource(1)), DefaultOptions)
	return e
}

type trk struct {
	id, title, artist, album string
	year                     int
	genres                   []string
	durMs                    int
	addedAt                  int64
}

func (e *env) add(t *testing.T, tracks ...trk) {
	t.Helper()
	ctx := context.Background()
	for _, x := range tracks {
		dur := x.durMs
		if dur == 0 {
			dur = 200_000
		}
		album := x.album
		if album == "" {
			album = x.artist + " LP"
		}
		gs := ""
		for i, g := range x.genres {
			if i > 0 {
				gs += "; "
			}
			gs += g
		}
		if err := e.q.UpsertLibraryTrack(ctx, db.UpsertLibraryTrackParams{
			LibraryTrackID: x.id, Title: x.title, Artist: x.artist, ArtistKey: matching.ArtistKey(x.artist), ArtistID: "ar-" + matching.ArtistKey(x.artist),
			Album: album, AlbumID: "al-" + album, DurationMs: int64(dur), Year: int64(x.year), Genres: gs, AddedAt: x.addedAt, Epoch: 1,
		}); err != nil {
			t.Fatal(err)
		}
		for _, g := range x.genres {
			if err := e.q.InsertLibraryTrackGenre(ctx, db.InsertLibraryTrackGenreParams{LibraryTrackID: x.id, GenreKey: matching.GenreKey(g)}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// artistTracks adds n tracks by an artist, one album per 4 tracks.
func (e *env) artistTracks(t *testing.T, artist string, n, year int, genres ...string) {
	t.Helper()
	for i := 0; i < n; i++ {
		e.add(t, trk{
			id: fmt.Sprintf("%s-%d", matching.ArtistKey(artist), i), title: fmt.Sprintf("%s song %d", artist, i), artist: artist,
			album: fmt.Sprintf("%s album %d", artist, i/4), year: year, genres: genres,
		})
	}
}

func (e *env) next(t *testing.T, req Request) Result {
	t.Helper()
	res, err := e.eng.Next(context.Background(), "u1", req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func ids(items []Candidate) []string {
	var out []string
	for _, c := range items {
		if c.Track != nil {
			out = append(out, c.Track.ID)
		} else {
			out = append(out, "ext:"+c.External.ExternalID)
		}
	}
	return out
}

func seedBonobo(t *testing.T, e *env) {
	e.artistTracks(t, "Bonobo", 8, 2017, "Electronic", "Downtempo")
	e.artistTracks(t, "Tycho", 8, 2016, "Electronic", "Ambient")
	e.artistTracks(t, "Four Tet", 8, 2015, "Electronic")
	e.artistTracks(t, "Metallica", 8, 1991, "Metal")
	e.sim["bonobo"] = []libindex.SimilarArtist{
		{Key: "tycho", Name: "Tycho", OwnedTracks: 8, Rank: 0, Source: "deezer", ExternalID: "d-tycho"},
		{Key: "four tet", Name: "Four Tet", OwnedTracks: 8, Rank: 1, Source: "deezer", ExternalID: "d-fourtet"},
		{Key: "emancipator", Name: "Emancipator", OwnedTracks: 0, Rank: 2, Source: "deezer", ExternalID: "d-eman"},
	}
}

func TestArtistRadio_DrawsFromSeedAndSimilarArtistsWithReasons(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	res := e.next(t, Request{Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 15})
	if res.Station.ID != "artist:bonobo" || res.Station.Label != "Bonobo radio" {
		t.Fatalf("station = %+v", res.Station)
	}
	if len(res.Items) != 15 {
		t.Fatalf("got %d items, want 15: %v", len(res.Items), ids(res.Items))
	}
	kinds := map[string]int{}
	for _, c := range res.Items {
		if !c.Owned || c.Track == nil {
			t.Fatalf("radio returned a song that isn't in the library: %+v", c)
		}
		if c.Reason.ArtistKey == "metallica" && c.Reason.Kind != ReasonLibrary {
			t.Errorf("metallica picked with reason %+v", c.Reason)
		}
		kinds[c.Reason.Kind]++
		if c.Reason.Kind == ReasonSimilarArtist && c.Reason.SeedArtist != "Bonobo" {
			t.Errorf("similar reason should name the seed artist: %+v", c.Reason)
		}
	}
	if kinds[ReasonSeedArtist] == 0 || kinds[ReasonSimilarArtist] == 0 {
		t.Errorf("want both seed-artist and similar-artist picks, got %v", kinds)
	}
}

func TestRadio_SpreadsArtistsAndCapsAlbums(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	res := e.next(t, Request{Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 12})
	perArtist := map[string]int{}
	perAlbum := map[string]int{}
	for i, c := range res.Items {
		perArtist[c.artistKey]++
		perAlbum[c.Track.AlbumID]++
		if i > 0 && res.Items[i-1].artistKey == c.artistKey {
			t.Errorf("same artist back to back at %d: %v", i, ids(res.Items))
		}
	}
	for a, n := range perArtist {
		if n > DefaultOptions.MaxPerArtist+1 {
			t.Errorf("artist %s picked %d times", a, n)
		}
	}
	for a, n := range perAlbum {
		if n > DefaultOptions.MaxPerAlbum {
			t.Errorf("album %s picked %d times", a, n)
		}
	}
}

func TestTrackRadio_StartsWithTheSeedAndNeverRepeatsIt(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	res := e.next(t, Request{Seed: Seed{Kind: SeedTrack, ID: "bonobo-0"}, N: 10})
	if res.Items[0].Track.ID != "bonobo-0" || res.Items[0].Reason.Kind != ReasonSeed {
		t.Fatalf("first item = %+v", res.Items[0])
	}
	for _, c := range res.Items[1:] {
		if c.Track.ID == "bonobo-0" {
			t.Fatal("seed track repeated")
		}
	}
	if res.Station.Label != "Bonobo song 0 radio" {
		t.Errorf("label = %q", res.Station.Label)
	}
	// A refill excludes the queue, so the seed isn't prepended again.
	more := e.next(t, Request{Seed: Seed{Kind: SeedTrack, ID: "bonobo-0"}, Exclude: ids(res.Items), N: 10})
	for _, c := range more.Items {
		for _, id := range ids(res.Items) {
			if c.Track.ID == id {
				t.Fatalf("refill repeated %s", id)
			}
		}
	}
}

func TestRadio_NeverPicksThumbedDownOrRecentOrShortSongs(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	e.add(t, trk{id: "skit", title: "Interlude", artist: "Tycho", durMs: 20_000, genres: []string{"Electronic"}})
	cat := func(id string) string { return "cat-" + id }
	e.taste.in = taste.Inputs{
		Listens: []taste.Listen{
			// Heard an hour ago: too recent for radio.
			{CatalogID: cat("bonobo-1"), LibraryTrackID: "bonobo-1", Title: "Bonobo song 1", Artist: "Bonobo", StartedAt: now.Add(-time.Hour).Unix(), Outcome: taste.OutcomeCompleted, DurationMs: 200_000, MsPlayed: 200_000},
		},
		Feedback: []taste.Feedback{
			{TargetKind: taste.TargetArtist, TargetKey: "four tet"},
			{TargetKind: taste.TargetTrack, TargetKey: cat("tycho-2")},
		},
		Plays: []taste.Play{{CatalogID: cat("tycho-2"), Title: "Tycho song 2", Artist: "Tycho", PlayedAt: now.Add(-48 * time.Hour).Unix()}},
	}
	for i := 0; i < 5; i++ {
		res := e.next(t, Request{Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 20})
		for _, c := range res.Items {
			switch {
			case c.artistKey == "four tet":
				t.Fatal("picked a thumbed-down artist")
			case c.Track.ID == "tycho-2":
				t.Fatal("picked a thumbed-down song")
			case c.Track.ID == "bonobo-1":
				t.Fatal("picked a song heard an hour ago")
			case c.Track.ID == "skit":
				t.Fatal("picked a 20-second interlude")
			}
		}
	}
}

func TestRadio_TasteTiltsTheDraw(t *testing.T) {
	e := newEnv(t)
	e.eng.opt.Temperature = 0.02 // near-greedy
	e.artistTracks(t, "Loved", 6, 2010, "Rock")
	e.artistTracks(t, "Meh", 6, 2010, "Rock")
	var listens []taste.Listen
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("loved-%d", i)
		listens = append(listens, taste.Listen{CatalogID: "c" + id, LibraryTrackID: id, Title: fmt.Sprintf("Loved song %d", i), Artist: "Loved",
			StartedAt: now.Add(-72 * time.Hour).Unix(), Outcome: taste.OutcomeCompleted, DurationMs: 200_000, MsPlayed: 200_000})
	}
	e.taste.in = taste.Inputs{Listens: listens}
	for i := 0; i < 5; i++ {
		res := e.next(t, Request{Seed: Seed{Kind: SeedGenre, ID: "rock"}, N: 4})
		// Back-to-back artists are avoided, so the loved artist leads and
		// alternates rather than filling the batch.
		if res.Items[0].artistKey != "loved" || res.Items[2].artistKey != "loved" {
			t.Fatalf("near-greedy draw should lead with the loved artist, got %v", ids(res.Items))
		}
	}
}

func TestGenreRadio_StaysInTheGenre(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	res := e.next(t, Request{Seed: Seed{Kind: SeedGenre, ID: "Metal"}, N: 10})
	if res.Station.Label != "Metal radio" || res.Station.ID != "genre:metal" {
		t.Fatalf("station = %+v", res.Station)
	}
	if len(res.Items) == 0 {
		t.Fatal("no items")
	}
	for _, c := range res.Items {
		if c.artistKey != "metallica" {
			t.Fatalf("genre radio left the genre: %v", ids(res.Items))
		}
		if c.Reason.Kind != ReasonGenre || c.Reason.Genre != "Metal" {
			t.Errorf("reason = %+v", c.Reason)
		}
	}
}

func TestGenreRadio_WithAPeriodStaysInIt(t *testing.T) {
	e := newEnv(t)
	e.artistTracks(t, "Old Band", 6, 1994, "Rock")
	e.artistTracks(t, "New Band", 6, 2021, "Rock")
	res := e.next(t, Request{Seed: Seed{Kind: SeedGenre, ID: "Rock"}, FromYear: 1990, ToYear: 1999, N: 10})
	if len(res.Items) == 0 {
		t.Fatal("no items")
	}
	for _, c := range res.Items {
		if c.artistKey != "old band" {
			t.Fatalf("left the period: %v", ids(res.Items))
		}
	}
}

func TestLanes(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	e.add(t, trk{id: "fresh-1", title: "Brand New", artist: "Newcomer", addedAt: now.Add(-24 * time.Hour).Unix()})
	listen := func(id, title, artist string, n int) []taste.Listen {
		var out []taste.Listen
		for i := 0; i < n; i++ {
			out = append(out, taste.Listen{CatalogID: "c" + id, LibraryTrackID: id, Title: title, Artist: artist,
				StartedAt: now.Add(-time.Duration(30+i) * time.Hour).Unix(), Outcome: taste.OutcomeCompleted, DurationMs: 200_000, MsPlayed: 200_000})
		}
		return out
	}
	var ls []taste.Listen
	ls = append(ls, listen("bonobo-0", "Bonobo song 0", "Bonobo", 6)...)
	ls = append(ls, listen("bonobo-1", "Bonobo song 1", "Bonobo", 4)...)
	ls = append(ls, listen("tycho-0", "Tycho song 0", "Tycho", 1)...)
	e.taste.in = taste.Inputs{Listens: ls}

	t.Run("on-repeat plays the most-played songs with counts", func(t *testing.T) {
		res := e.next(t, Request{Lane: LaneOnRepeat, N: 5})
		if res.Station.Label != "On repeat" {
			t.Errorf("label %q", res.Station.Label)
		}
		got := map[string]Candidate{}
		for _, c := range res.Items {
			got[c.Track.ID] = c
		}
		if got["bonobo-0"].Reason.PlayCount != 6 || got["bonobo-0"].Reason.Kind != ReasonOnRepeat {
			t.Errorf("bonobo-0 = %+v (all %v)", got["bonobo-0"].Reason, ids(res.Items))
		}
		if len(res.Items) != 3 {
			t.Errorf("only played songs belong on repeat: %v", ids(res.Items))
		}
	})
	t.Run("deep cuts are favourite artists' less-played songs", func(t *testing.T) {
		res := e.next(t, Request{Lane: LaneDeepCuts, N: 6})
		for _, c := range res.Items {
			if c.Track.ID == "bonobo-0" || c.Track.ID == "bonobo-1" {
				t.Errorf("deep cuts picked a well-worn song %s", c.Track.ID)
			}
			if c.artistKey != "bonobo" && c.artistKey != "tycho" {
				t.Errorf("deep cut from a non-favourite %s", c.artistKey)
			}
		}
	})
	t.Run("new to you never repeats a played song", func(t *testing.T) {
		res := e.next(t, Request{Lane: LaneNewToYou, N: 20})
		for _, c := range res.Items {
			if c.Track.ID == "bonobo-0" || c.Track.ID == "bonobo-1" || c.Track.ID == "tycho-0" {
				t.Errorf("new-to-you picked %s", c.Track.ID)
			}
		}
	})
	t.Run("sounds like leaves the seed artist out", func(t *testing.T) {
		res := e.next(t, Request{Lane: LaneSoundsLike, Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 10})
		if res.Station.Label != "Sounds like Bonobo" {
			t.Errorf("label %q", res.Station.Label)
		}
		for _, c := range res.Items {
			if c.artistKey == "bonobo" {
				t.Fatal("sounds-like picked the seed artist")
			}
		}
		// No seed: like the user's top artist.
		res = e.next(t, Request{Lane: LaneSoundsLike, N: 5})
		if res.Station.Label != "Sounds like Bonobo" {
			t.Errorf("seedless label %q", res.Station.Label)
		}
	})
	t.Run("throwback stays in the period", func(t *testing.T) {
		res := e.next(t, Request{Lane: LaneThrowback, FromYear: 1990, ToYear: 1999, N: 5})
		if res.Station.Label != "Throwback: 1990–1999" || len(res.Items) == 0 {
			t.Fatalf("station %+v items %v", res.Station, ids(res.Items))
		}
		for _, c := range res.Items {
			if c.Year < 1990 || c.Year > 1999 || c.Reason.Kind != ReasonThrowback {
				t.Errorf("out of period: %+v", c)
			}
		}
	})
	t.Run("fresh finds lead with what was just added", func(t *testing.T) {
		res := e.next(t, Request{Lane: LaneFreshFinds, N: 3})
		if len(res.Items) != 1 || res.Items[0].Track.ID != "fresh-1" || res.Items[0].Reason.Kind != ReasonFresh {
			t.Fatalf("fresh = %v", ids(res.Items))
		}
	})
}

func TestNext_RejectsBadRequests(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	ctx := context.Background()
	for name, req := range map[string]Request{
		"unknown lane":        {Lane: "vibes"},
		"throwback no years":  {Lane: LaneThrowback},
		"unknown seed kind":   {Seed: Seed{Kind: "planet"}},
		"track seed no id":    {Seed: Seed{Kind: SeedTrack}},
		"sounds-like no data": {Lane: LaneSoundsLike},
	} {
		if _, err := e.eng.Next(ctx, "u1", req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := e.eng.Next(ctx, "u1", Request{Seed: Seed{Kind: SeedTrack, ID: "nope"}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing seed track: err = %v", err)
	}
	// A song that isn't indexed but whose artist is known becomes that artist's station.
	res, err := e.eng.Next(ctx, "u1", Request{Seed: Seed{Kind: SeedTrack, ID: "trk_123", Title: "Kerala", Artist: "Bonobo"}, N: 5})
	if err != nil {
		t.Fatalf("unindexed track with artist: %v", err)
	}
	if res.Station.ID != "track:trk_123" || res.Station.Label != "Kerala radio" || len(res.Items) == 0 {
		t.Fatalf("fallback station = %+v, %d items", res.Station, len(res.Items))
	}
	for _, c := range res.Items {
		if c.Reason.Kind == ReasonSeed {
			t.Fatal("no seed song to start with when it isn't in the library")
		}
	}
}

func TestDiscovery_AddsSongsNotInTheLibraryOnlyWhenAsked(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	res := e.next(t, Request{Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 8})
	for _, c := range res.Items {
		if !c.Owned {
			t.Fatal("external song without IncludeExternal")
		}
	}
	if e.top.calls != 0 {
		t.Fatal("asked the external source without IncludeExternal")
	}
	res = e.next(t, Request{Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 8, IncludeExternal: true})
	var ext []Candidate
	for _, c := range res.Items {
		if !c.Owned {
			ext = append(ext, c)
		}
	}
	if len(ext) != 1 || ext[0].External.ExternalID != "t-d-eman" {
		t.Fatalf("external = %+v", ext)
	}
	r := ext[0].Reason
	if r.Kind != ReasonDiscovery || r.Artist != "Emancipator" || r.SeedArtist != "Bonobo" {
		t.Errorf("reason = %+v", r)
	}
	if len(res.Items) != 8 {
		t.Errorf("len = %d", len(res.Items))
	}
}

func TestSuggestions_FavouriteArtistsThenTheirGenres(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	// No history: the library's biggest genres.
	sug, err := e.eng.Suggestions(context.Background(), "u1", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(sug) == 0 || sug[0].Seed.Kind != SeedGenre || sug[0].Label != "Electronic radio" {
		t.Fatalf("cold-start suggestions = %+v", sug)
	}
	e.taste.in = taste.Inputs{Listens: []taste.Listen{{CatalogID: "c1", LibraryTrackID: "metallica-0", Title: "Metallica song 0", Artist: "Metallica",
		StartedAt: now.Add(-48 * time.Hour).Unix(), Outcome: taste.OutcomeCompleted, DurationMs: 200_000, MsPlayed: 200_000}}}
	sug, err = e.eng.Suggestions(context.Background(), "u1", 4)
	if err != nil {
		t.Fatal(err)
	}
	if sug[0].Seed != (Seed{Kind: SeedArtist, ID: "metallica", Artist: "Metallica"}) || sug[0].LibraryArtistID != "ar-metallica" {
		t.Fatalf("first suggestion = %+v", sug[0])
	}
	if len(sug) < 2 || sug[1].Seed.Kind != SeedGenre || sug[1].Label != "Metal radio" {
		t.Fatalf("second suggestion should be the favourite's genre: %+v", sug)
	}
}

func TestGenres_ListsLibraryGenresWithLabels(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	gs, err := e.eng.Genres(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) == 0 || gs[0].Key != "electronic" || gs[0].Label != "Electronic" || gs[0].Tracks != 24 {
		t.Fatalf("genres = %+v", gs)
	}
}

func TestDiscover_NewArtistsFromFavouritesSkippingExcluded(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	e.sim["bonobo"] = append(e.sim["bonobo"], libindex.SimilarArtist{Key: "nightmares on wax", Name: "Nightmares On Wax", Rank: 3, Source: "deezer", ExternalID: "d-now"})
	var ls []taste.Listen
	for i := 0; i < 6; i++ {
		ls = append(ls, taste.Listen{CatalogID: fmt.Sprintf("c%d", i), LibraryTrackID: fmt.Sprintf("bonobo-%d", i), Title: fmt.Sprintf("Bonobo song %d", i), Artist: "Bonobo",
			StartedAt: now.Add(-48 * time.Hour).Unix(), Outcome: taste.OutcomeCompleted, DurationMs: 200_000, MsPlayed: 200_000})
	}
	e.taste.in = taste.Inputs{Listens: ls}
	got, err := e.eng.Discover(context.Background(), "u1", 5, map[string]bool{"deezer:t-d-eman": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Owned || got[0].External.ExternalID != "t-d-now" {
		t.Fatalf("discover = %+v", ids(got))
	}
	if r := got[0].Reason; r.Kind != ReasonDiscovery || r.SeedArtist != "Bonobo" || r.Artist != "Nightmares On Wax" {
		t.Errorf("reason = %+v", r)
	}
	// No history, nothing to discover from.
	e.taste.in = taste.Inputs{}
	if got, _ := e.eng.Discover(context.Background(), "u1", 5, nil); len(got) != 0 {
		t.Errorf("cold start discover = %v", ids(got))
	}
}

type fakeSonic struct {
	ready bool
	near  map[string][]analysis.Neighbor
	asked [][]string
}

func (f *fakeSonic) Ready(context.Context) bool { return f.ready }
func (f *fakeSonic) SimilarTo(_ context.Context, ids []string, n int) ([]analysis.Neighbor, error) {
	f.asked = append(f.asked, ids)
	out := f.near[ids[0]]
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func TestSound_TrackRadioAndSoundsLikeUseAudioAnalysisWhenReady(t *testing.T) {
	e := newEnv(t)
	seedBonobo(t, e)
	// Metallica's songs "sound like" Bonobo song 0 in this made-up analysis,
	// so only audio analysis can bring them in.
	sonic := &fakeSonic{near: map[string][]analysis.Neighbor{"bonobo-0": {{ID: "metallica-0", Score: 0.9}, {ID: "metallica-1", Score: 0.8}}}}
	e.eng.SetSonic(sonic)
	res := e.next(t, Request{Seed: Seed{Kind: SeedTrack, ID: "bonobo-0"}, Lane: LaneSoundsLike, N: 10})
	for _, c := range res.Items {
		if c.Reason.Kind == ReasonSound {
			t.Fatal("used audio analysis before it was ready")
		}
	}
	sonic.ready = true
	found := 0
	for i := 0; i < 5; i++ {
		res = e.next(t, Request{Seed: Seed{Kind: SeedTrack, ID: "bonobo-0"}, Lane: LaneSoundsLike, N: 10})
		for _, c := range res.Items {
			if c.Reason.Kind == ReasonSound {
				found++
				if c.Reason.SeedTitle != "Bonobo song 0" || c.Reason.Artist != "Metallica" {
					t.Fatalf("reason = %+v", c.Reason)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no sounds-like picks from audio analysis")
	}
	// Artist radio asks with a handful of the artist's songs.
	sonic.asked = nil
	e.next(t, Request{Seed: Seed{Kind: SeedArtist, ID: "bonobo"}, N: 10})
	if len(sonic.asked) != 1 || len(sonic.asked[0]) != 8 {
		t.Fatalf("asked = %v", sonic.asked)
	}
}
