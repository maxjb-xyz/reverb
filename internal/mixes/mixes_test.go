package mixes

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/engine"
	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

type fakeTaste struct{ in taste.Inputs }

func (f *fakeTaste) Profile(context.Context, string) (*taste.Profile, error) {
	return taste.Build("u1", time.Unix(1_800_000_000, 0), f.in), nil
}

type fakeSimilar map[string][]libindex.SimilarArtist

func (f fakeSimilar) Similar(_ context.Context, key string) ([]libindex.SimilarArtist, error) {
	return f[key], nil
}

type countingEngine struct {
	*engine.Engine
	calls int
}

func (c *countingEngine) Next(ctx context.Context, u string, r engine.Request) (engine.Result, error) {
	c.calls++
	return c.Engine.Next(ctx, u, r)
}

type env struct {
	q     *db.Queries
	svc   *Service
	eng   *countingEngine
	taste *fakeTaste
	sim   fakeSimilar
	now   time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/mixes.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := st.Q()
	if err := q.CreateRole(ctx, db.CreateRoleParams{ID: "r", Name: "User", Capabilities: `[]`}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"u1", "u2"} {
		if err := q.CreateUser(ctx, db.CreateUserParams{ID: u, Username: u, PasswordHash: "x", RoleID: "r"}); err != nil {
			t.Fatal(err)
		}
	}
	e := &env{q: q, taste: &fakeTaste{}, sim: fakeSimilar{}, now: time.Date(2027, 1, 14, 12, 0, 0, 0, time.Local)}
	clock := func() time.Time { return e.now }
	e.eng = &countingEngine{Engine: engine.New(q, e.taste, e.sim, nil, clock, rand.New(rand.NewSource(7)), engine.DefaultOptions)}
	n := 0
	e.svc = New(q, e.eng, e.taste, e.sim, clock, func() string { n++; return fmt.Sprintf("mix-%d", n) })
	return e
}

func (e *env) artist(t *testing.T, name string, n int, genre string) {
	t.Helper()
	key := matching.ArtistKey(name)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%d", key, i)
		if err := e.q.UpsertLibraryTrack(context.Background(), db.UpsertLibraryTrackParams{
			LibraryTrackID: id, Title: fmt.Sprintf("%s song %d", name, i), Artist: name, ArtistKey: key, ArtistID: "ar-" + key,
			Album: fmt.Sprintf("%s album %d", name, i/3), AlbumID: fmt.Sprintf("al-%s-%d", key, i/3), CoverArtID: "cov-" + key,
			DurationMs: 200_000, Genres: genre, Epoch: 1,
		}); err != nil {
			t.Fatal(err)
		}
		if err := e.q.InsertLibraryTrackGenre(context.Background(), db.InsertLibraryTrackGenreParams{LibraryTrackID: id, GenreKey: matching.GenreKey(genre)}); err != nil {
			t.Fatal(err)
		}
	}
}

// likes gives the user a history with these artists (more listens = stronger).
func (e *env) likes(artists map[string]int) {
	var ls []taste.Listen
	for name, n := range artists {
		key := matching.ArtistKey(name)
		for i := 0; i < n; i++ {
			ls = append(ls, taste.Listen{CatalogID: fmt.Sprintf("c-%s-%d", key, i), Title: fmt.Sprintf("old %d", i), Artist: name,
				StartedAt: e.now.Add(-72 * time.Hour).Unix(), Outcome: taste.OutcomeCompleted, DurationMs: 200_000, MsPlayed: 200_000})
		}
	}
	e.taste.in = taste.Inputs{Listens: ls}
}

func library(t *testing.T, e *env) {
	for _, a := range []string{"Bonobo", "Tycho", "Four Tet"} {
		e.artist(t, a, 12, "Electronic")
	}
	for _, a := range []string{"Metallica", "Megadeth"} {
		e.artist(t, a, 12, "Metal")
	}
	e.artist(t, "Miles Davis", 12, "Jazz")
	e.sim["bonobo"] = []libindex.SimilarArtist{{Key: "tycho", Name: "Tycho", OwnedTracks: 12}}
}

func TestDailyMixes_ClusterFavouritesBySimilarityAndGenre(t *testing.T) {
	e := newEnv(t)
	library(t, e)
	e.likes(map[string]int{"Bonobo": 9, "Metallica": 8, "Tycho": 6, "Megadeth": 5, "Four Tet": 4, "Miles Davis": 3})
	mixes, err := e.svc.List(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(mixes) != 3 {
		t.Fatalf("want 3 daily mixes (electronic, metal, jazz), got %d: %+v", len(mixes), names(mixes))
	}
	first := mixes[0]
	if first.Kind != KindDaily || first.Name != "Daily Mix 1" {
		t.Fatalf("first = %s %s", first.Kind, first.Name)
	}
	seeds := map[string]bool{}
	for _, s := range first.Recipe.Seeds {
		seeds[s.ID] = true
	}
	if !seeds["bonobo"] || !seeds["tycho"] || !seeds["four tet"] || seeds["metallica"] {
		t.Errorf("electronic cluster seeds = %+v", first.Recipe.Seeds)
	}
	if first.Description != "Bonobo, Tycho, Four Tet and more" {
		t.Errorf("description = %q", first.Description)
	}
	if len(first.Items) != DefaultSize || first.CoverArtID == "" {
		t.Errorf("items = %d cover = %q", len(first.Items), first.CoverArtID)
	}
	seen := map[string]bool{}
	for _, c := range first.Items {
		if seen[c.Track.ID] {
			t.Fatalf("duplicate %s", c.Track.ID)
		}
		seen[c.Track.ID] = true
	}
	if mixes[1].Recipe.Seeds[0].ID != "metallica" || len(mixes[1].Recipe.Seeds) != 2 {
		t.Errorf("metal cluster = %+v", mixes[1].Recipe.Seeds)
	}
}

func TestDailyMixes_RebuildOncePerDay(t *testing.T) {
	e := newEnv(t)
	library(t, e)
	e.likes(map[string]int{"Bonobo": 5})
	ctx := context.Background()
	m1, err := e.svc.List(ctx, "u1")
	if err != nil || len(m1) != 1 {
		t.Fatalf("list = %v %v", names(m1), err)
	}
	calls := e.eng.calls
	e.now = e.now.Add(6 * time.Hour) // same day
	if _, err := e.svc.List(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if e.eng.calls != calls {
		t.Fatal("rebuilt twice in one day")
	}
	e.now = e.now.Add(24 * time.Hour)
	m2, err := e.svc.List(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if e.eng.calls == calls || len(m2) != 1 || m2[0].GeneratedAt != e.now.Unix() {
		t.Fatalf("not rebuilt the next day: %+v", m2)
	}
	// Another user's mixes are their own.
	other, err := e.svc.List(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Fatalf("u2 = %v", names(other))
	}
	if _, err := e.svc.Get(ctx, "u2", m2[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("u2 read u1's mix: %v", err)
	}
}

func TestDailyMixes_NoneWithoutHistory(t *testing.T) {
	e := newEnv(t)
	library(t, e)
	mixes, err := e.svc.List(context.Background(), "u1")
	if err != nil || len(mixes) != 0 {
		t.Fatalf("mixes = %v err %v", names(mixes), err)
	}
}

func TestCustomMix_CreateRefreshUpdateDelete(t *testing.T) {
	e := newEnv(t)
	library(t, e)
	ctx := context.Background()
	m, err := e.svc.Create(ctx, "u1", "  Late night  ", Recipe{
		Seeds: []engine.Seed{{Kind: engine.SeedArtist, ID: "bonobo"}, {Kind: engine.SeedGenre, ID: "Jazz"}},
		Size:  12, Refresh: RefreshWeekly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "Late night" || m.Kind != KindCustom || len(m.Items) != 12 {
		t.Fatalf("created %+v (%d items)", m, len(m.Items))
	}
	jazz, electronic := 0, 0
	for _, c := range m.Items {
		switch c.Reason.Genre {
		case "Jazz":
			jazz++
		}
		if c.Reason.Kind == engine.ReasonSeedArtist || c.Reason.Kind == engine.ReasonSimilarArtist {
			electronic++
		}
	}
	if jazz == 0 || electronic == 0 {
		t.Errorf("both seeds should be heard: jazz=%d bonobo-ish=%d", jazz, electronic)
	}

	// Weekly: not rebuilt after a day, rebuilt after a week.
	calls := e.eng.calls
	e.now = e.now.Add(48 * time.Hour)
	if _, err := e.svc.Get(ctx, "u1", m.ID); err != nil || e.eng.calls != calls {
		t.Fatalf("weekly mix rebuilt early (err %v)", err)
	}
	e.now = e.now.Add(6 * 24 * time.Hour)
	got, err := e.svc.Get(ctx, "u1", m.ID)
	if err != nil || e.eng.calls == calls || got.GeneratedAt != e.now.Unix() {
		t.Fatalf("weekly mix not rebuilt after a week (err %v)", err)
	}

	up, err := e.svc.Update(ctx, "u1", m.ID, "Metal only", Recipe{Seeds: []engine.Seed{{Kind: engine.SeedGenre, ID: "metal"}}, Size: 10, Refresh: RefreshManual})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range up.Items {
		if c.Reason.Genre != "Metal" {
			t.Fatalf("updated mix left its genre: %+v", c.Reason)
		}
	}
	calls = e.eng.calls
	e.now = e.now.Add(30 * 24 * time.Hour)
	if _, err := e.svc.Get(ctx, "u1", m.ID); err != nil || e.eng.calls != calls {
		t.Fatal("manual mix rebuilt on its own")
	}
	if _, err := e.svc.Refresh(ctx, "u1", m.ID); err != nil || e.eng.calls == calls {
		t.Fatalf("refresh: %v", err)
	}
	if err := e.svc.Delete(ctx, "u2", m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("u2 deleted u1's mix: %v", err)
	}
	if err := e.svc.Delete(ctx, "u1", m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, "u1", m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestCustomMix_LanesAndValidation(t *testing.T) {
	e := newEnv(t)
	library(t, e)
	ctx := context.Background()
	for name, rec := range map[string]Recipe{
		"bad lane":         {Lane: "vibes"},
		"bad refresh":      {Refresh: "hourly"},
		"bad seed kind":    {Seeds: []engine.Seed{{Kind: "planet"}}},
		"empty artist":     {Seeds: []engine.Seed{{Kind: engine.SeedArtist}}},
		"throwback no era": {Lane: engine.LaneThrowback},
		"too many seeds":   {Seeds: make([]engine.Seed, MaxSeeds+1)},
	} {
		if _, err := e.svc.Create(ctx, "u1", "x", rec); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := e.svc.Create(ctx, "u1", "   ", Recipe{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("blank name: %v", err)
	}
	// No seeds = the user's taste; size is clamped.
	m, err := e.svc.Create(ctx, "u1", "Everything", Recipe{Size: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if m.Recipe.Size != MaxSize || m.Recipe.Refresh != RefreshDaily || m.Recipe.Seeds[0].Kind != engine.SeedTaste {
		t.Errorf("normalized recipe = %+v", m.Recipe)
	}
	// Daily mixes are Reverb's: read-only.
	e.likes(map[string]int{"Bonobo": 5})
	all, err := e.svc.List(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	var daily Mix
	for _, x := range all {
		if x.Kind == KindDaily {
			daily = x
		}
	}
	if daily.ID == "" {
		t.Fatalf("no daily mix in %v", names(all))
	}
	if all[0].Kind != KindDaily {
		t.Errorf("daily mixes should list first: %v", names(all))
	}
	if _, err := e.svc.Update(ctx, "u1", daily.ID, "Mine", Recipe{}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("update daily: %v", err)
	}
	if err := e.svc.Delete(ctx, "u1", daily.ID); !errors.Is(err, ErrReadOnly) {
		t.Errorf("delete daily: %v", err)
	}
	if _, err := e.svc.Refresh(ctx, "u1", daily.ID); err != nil {
		t.Errorf("refreshing a daily mix should work: %v", err)
	}
}

func names(ms []Mix) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Kind+":"+m.Name)
	}
	return out
}
