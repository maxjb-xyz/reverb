package libindex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type fakeLib struct {
	library.LibraryAdapter
	batches [][]library.IndexedTrack
	err     error
}

func (f *fakeLib) WalkTracks(_ context.Context, fn func([]library.IndexedTrack) error) error {
	for _, b := range f.batches {
		if err := fn(b); err != nil {
			return err
		}
	}
	return f.err
}

type fakeSrc struct {
	search.SearchSource
	artists  map[string]string   // name -> external id
	similar  map[string][]string // external id -> similar names
	searches int
	failFor  string
}

func (f *fakeSrc) Name() string { return "deezer" }
func (f *fakeSrc) Search(_ context.Context, q string, _ core.EntityType) ([]core.ExternalResult, error) {
	f.searches++
	if q == f.failFor {
		return nil, errors.New("rate limited")
	}
	if id, ok := f.artists[q]; ok {
		return []core.ExternalResult{{Title: q + " Tribute Band", ExternalID: "x"}, {Title: q, ExternalID: id}}, nil
	}
	return []core.ExternalResult{{Title: "Somebody Else", ExternalID: "zzz"}}, nil
}
func (f *fakeSrc) SimilarArtists(_ context.Context, id string, _ int) ([]core.ExternalArtist, error) {
	var out []core.ExternalArtist
	for i, n := range f.similar[id] {
		out = append(out, core.ExternalArtist{Source: "deezer", ExternalID: "s" + string(rune('a'+i)), Name: n})
	}
	return out, nil
}

func tr(id, title, artist string, year int, genres ...string) library.IndexedTrack {
	return library.IndexedTrack{Track: core.Track{ID: id, Title: title, Artist: artist, Album: "A", DurationMs: 200_000}, Year: year, Genres: genres, AddedAt: 1_700_000_000}
}

type env struct {
	svc *Service
	q   *db.Queries
	lib *fakeLib
	src *fakeSrc
	now time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/idx.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	e := &env{lib: &fakeLib{}, src: &fakeSrc{artists: map[string]string{}, similar: map[string][]string{}}, now: time.Unix(1_800_000_000, 0)}
	e.q = st.Q()
	e.svc = New(st.DB(), func() (library.LibraryAdapter, []search.SearchSource) {
		return e.lib, []search.SearchSource{e.src}
	}, func() time.Time { return e.now }, Options{}).NoPace()
	return e
}

func TestRun_IndexesTracksWithFacets(t *testing.T) {
	e := newEnv(t)
	e.lib.batches = [][]library.IndexedTrack{
		{tr("t1", "Around the World", "Daft Punk", 1997, "Electronic", "House"), tr("t2", "Da Funk", "Daft Punk", 1995, "Electronic")},
		{tr("t3", "Song 2", "Blur", 1997, "Britpop")},
	}
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Tracks != 3 || st.Artists != 2 || st.LastIndexedAt == 0 || st.Running {
		t.Fatalf("status = %+v", st)
	}
	got, err := e.q.ListLibraryTracksByGenre(context.Background(), db.ListLibraryTracksByGenreParams{GenreKey: matching.GenreKey("electronic"), Limit: 10})
	if err != nil || len(got) != 2 {
		t.Fatalf("by genre = %+v %v", got, err)
	}
	byYear, _ := e.q.ListLibraryTracksByYearRange(context.Background(), db.ListLibraryTracksByYearRangeParams{Year: 1997, Year_2: 1997, Limit: 10})
	if len(byYear) != 2 {
		t.Fatalf("1997 tracks = %d, want 2", len(byYear))
	}
	row, _ := e.q.GetLibraryTrack(context.Background(), "t1")
	if row.ArtistKey != matching.ArtistKey("Daft Punk") || row.Genres != "Electronic; House" {
		t.Fatalf("row = %+v", row)
	}
}

func TestRun_DropsTracksNoLongerInLibrary(t *testing.T) {
	e := newEnv(t)
	e.lib.batches = [][]library.IndexedTrack{{tr("t1", "a", "X", 0, "Rock"), tr("t2", "b", "X", 0)}}
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Hour)
	e.lib.batches = [][]library.IndexedTrack{{tr("t2", "b", "X", 0)}}
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := e.svc.Status(context.Background())
	if st.Tracks != 1 {
		t.Fatalf("tracks = %d, want 1 after t1 left the library", st.Tracks)
	}
	if g, _ := e.q.ListLibraryGenres(context.Background(), 10); len(g) != 0 {
		t.Fatalf("orphan genres left behind: %+v", g)
	}
}

func TestRun_FailedOrEmptyWalkKeepsIndex(t *testing.T) {
	e := newEnv(t)
	e.lib.batches = [][]library.IndexedTrack{{tr("t1", "a", "X", 0)}}
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Hour)
	e.lib.batches = nil
	e.lib.err = errors.New("navidrome down")
	if err := e.svc.Run(context.Background()); err == nil {
		t.Fatal("walk error must surface")
	}
	st, _ := e.svc.Status(context.Background())
	if st.Tracks != 1 || st.LastError == "" {
		t.Fatalf("failed walk must keep the index and report the error: %+v", st)
	}
	e.lib.err = nil // empty walk, no error
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.svc.Status(context.Background()); st.Tracks != 1 {
		t.Fatalf("empty walk wiped the index: %+v", st)
	}
}

func TestRun_BuildsSimilarityMap(t *testing.T) {
	e := newEnv(t)
	e.lib.batches = [][]library.IndexedTrack{{tr("t1", "a", "Daft Punk", 0), tr("t2", "b", "Justice", 0), tr("t3", "c", "Nobody Knows Me", 0)}}
	e.src.artists["Daft Punk"] = "27"
	e.src.artists["Justice"] = "1"
	e.src.similar["27"] = []string{"Justice", "Air", "Daft Punk"} // self is dropped
	e.src.similar["1"] = []string{"Daft Punk"}
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	sim, err := e.svc.Similar(context.Background(), matching.ArtistKey("Daft Punk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sim) != 2 || sim[0].Name != "Justice" || sim[0].OwnedTracks != 1 || sim[1].Name != "Air" || sim[1].OwnedTracks != 0 {
		t.Fatalf("similar = %+v", sim)
	}
	// The unknown artist got a "no match" row so it isn't retried every run.
	ext, err := e.q.GetArtistExternal(context.Background(), db.GetArtistExternalParams{ArtistKey: matching.ArtistKey("Nobody Knows Me"), Source: "deezer"})
	if err != nil || ext.ExternalID != "" {
		t.Fatalf("no-match row = %+v %v", ext, err)
	}
	st, _ := e.svc.Status(context.Background())
	if st.ArtistsWithSimilar != 2 {
		t.Fatalf("artistsWithSimilar = %d", st.ArtistsWithSimilar)
	}

	// A second run inside the TTL makes no external calls.
	before := e.src.searches
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.src.searches != before {
		t.Fatalf("fresh entries were refetched (%d searches)", e.src.searches-before)
	}
	// After the TTL they are refreshed.
	e.now = e.now.Add(31 * 24 * time.Hour)
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.src.searches != before+3 {
		t.Fatalf("stale entries not refreshed: %d searches", e.src.searches-before)
	}
}

func TestRun_OneArtistFailingDoesNotStopTheMap(t *testing.T) {
	e := newEnv(t)
	e.lib.batches = [][]library.IndexedTrack{{tr("t1", "a", "Daft Punk", 0), tr("t2", "b", "Justice", 0)}}
	e.src.artists["Justice"] = "1"
	e.src.similar["1"] = []string{"Air"}
	e.src.failFor = "Daft Punk"
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sim, _ := e.svc.Similar(context.Background(), matching.ArtistKey("Justice")); len(sim) != 1 {
		t.Fatalf("Justice similar = %+v", sim)
	}
}

func TestRun_NoLibraryIsANoop(t *testing.T) {
	e := newEnv(t)
	e.svc.live = func() (library.LibraryAdapter, []search.SearchSource) { return nil, nil }
	if err := e.svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStatus_NamesTheSimilarArtistSources(t *testing.T) {
	e := newEnv(t)
	st, err := e.svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.SimilarSources) != 1 || st.SimilarSources[0] != "deezer" {
		t.Fatalf("similarSources = %v, want [deezer]", st.SimilarSources)
	}
	e.svc.live = func() (library.LibraryAdapter, []search.SearchSource) { return e.lib, nil }
	st, _ = e.svc.Status(context.Background())
	if st.SimilarSources == nil || len(st.SimilarSources) != 0 {
		t.Fatalf("similarSources with no source = %#v, want an empty list", st.SimilarSources)
	}
}
