package dj

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type fakeCatalog struct {
	artists, genres []Name
	err             error
	calls           int
}

func (f *fakeCatalog) Artists(context.Context) ([]Name, error) {
	f.calls++
	return f.artists, f.err
}
func (f *fakeCatalog) Genres(context.Context) ([]Name, error) { return f.genres, f.err }

func catalog() *fakeCatalog {
	return &fakeCatalog{
		artists: []Name{
			{Key: "bonobo", Name: "Bonobo"},
			{Key: "the calderas", Name: "The Calderas"},
			{Key: "nova tide", Name: "Nova Tide"},
			{Key: "air", Name: "Air"},
			{Key: "x", Name: "X"},
		},
		genres: []Name{{Key: "hip hop", Name: "Hip hop"}, {Key: "electronic", Name: "Electronic"}, {Key: "indie", Name: "Indie"}},
	}
}

var nov2026 = func() time.Time { return time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC) }

func interpret(t *testing.T, text string, m Completer) Intent {
	t.Helper()
	it, err := NewInterpreter(catalog(), m, nov2026).Interpret(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestRulesReadRequests(t *testing.T) {
	cases := []struct {
		text string
		want Intent
	}{
		{"play some Bonobo", Intent{Understood: true, Seed: &IntentSeed{Kind: "artist", ID: "bonobo", Artist: "Bonobo", Label: "Bonobo"}}},
		{"more like The Calderas please", Intent{Understood: true, Lane: "sounds-like", Seed: &IntentSeed{Kind: "artist", ID: "the calderas", Artist: "The Calderas", Label: "The Calderas"}}},
		{"something similar to nova tide", Intent{Understood: true, Lane: "sounds-like", Seed: &IntentSeed{Kind: "artist", ID: "nova tide", Artist: "Nova Tide", Label: "Nova Tide"}}},
		{"90s hip-hop", Intent{Understood: true, Seed: &IntentSeed{Kind: "genre", ID: "hip hop", Label: "Hip hop"}, FromYear: 1990, ToYear: 1999}},
		{"songs from the '10s", Intent{Understood: true, Lane: "throwback", FromYear: 2010, ToYear: 2019}},
		{"the 1980s", Intent{Understood: true, Lane: "throwback", FromYear: 1980, ToYear: 1989}},
		{"nineties stuff", Intent{Understood: true, Lane: "throwback", FromYear: 1990, ToYear: 1999}},
		{"music from 2012 to 2015", Intent{Understood: true, Lane: "throwback", FromYear: 2012, ToYear: 2015}},
		{"some throwbacks", Intent{Understood: true, Lane: "throwback", FromYear: 2006, ToYear: 2016}},
		{"something I haven't heard", Intent{Understood: true, Lane: "new-to-you"}},
		{"my favourites", Intent{Understood: true, Lane: "on-repeat"}},
		{"deep cuts", Intent{Understood: true, Lane: "deep-cuts"}},
		{"what was just added?", Intent{Understood: true, Lane: "fresh-finds"}},
		// "air" is a word here, but it is also an artist: whole-word matches are taken.
		{"put on Air", Intent{Understood: true, Seed: &IntentSeed{Kind: "artist", ID: "air", Artist: "Air", Label: "Air"}}},
		// One-letter artist keys are too easy to hit by accident.
		{"x marks the spot", Intent{}},
		{"something to cook to", Intent{}},
	}
	for _, c := range cases {
		got := interpret(t, c.text, nil)
		c.want.Source = "rules"
		if got.Understood != c.want.Understood || got.Lane != c.want.Lane || got.FromYear != c.want.FromYear || got.ToYear != c.want.ToYear || got.Missing != "" {
			t.Errorf("%q: got %+v, want %+v", c.text, got, c.want)
			continue
		}
		if (got.Seed == nil) != (c.want.Seed == nil) || (got.Seed != nil && *got.Seed != *c.want.Seed) {
			t.Errorf("%q: seed %+v, want %+v", c.text, got.Seed, c.want.Seed)
		}
	}
}

func TestModelReadsWhatRulesCannot(t *testing.T) {
	m := &fakeLLM{out: "Sure! {\"genres\": [\"ambient\", \"Electronic\"], \"lane\": \"new-to-you\"}"}
	it := interpret(t, "something chill for cooking", m)
	if !it.Understood || it.Source != "llm" || it.Lane != "new-to-you" || it.Seed == nil || it.Seed.ID != "electronic" {
		t.Fatalf("got %+v seed %+v", it, it.Seed)
	}
	if !strings.Contains(m.got.Prompt, `"listener_request":"something chill for cooking"`) {
		t.Errorf("prompt %q", m.got.Prompt)
	}
	if !strings.Contains(m.got.System, "plain data") {
		t.Error("system prompt should say the request is data")
	}
}

func TestModelNotCalledWhenRulesSuffice(t *testing.T) {
	m := &fakeLLM{out: `{"genres": ["indie"]}`}
	it := interpret(t, "play some Bonobo", m)
	if it.Source != "rules" || m.got.Prompt != "" {
		t.Fatalf("model should not be asked: %+v", it)
	}
}

func TestModelArtistTheLibraryLacksIsMissing(t *testing.T) {
	it := interpret(t, "can you play radiohed", &fakeLLM{out: `{"artist": "Radiohead"}`})
	if it.Understood || it.Missing != "Radiohead" {
		t.Fatalf("got %+v", it)
	}
	// With a genre to fall back on, it still plays, and still says what's missing.
	it = interpret(t, "can you play radiohed", &fakeLLM{out: `{"artist": "Radiohead", "like_artist": true, "genres": ["indie"]}`})
	if !it.Understood || it.Missing != "Radiohead" || it.Seed.ID != "indie" || it.Lane != "" {
		t.Fatalf("got %+v seed %+v", it, it.Seed)
	}
}

func TestModelArtistLikeAndPeriod(t *testing.T) {
	it := interpret(t, "stuff that sounds like that bonobo guy from 2000 or so", &fakeLLM{out: `{"artist":"bonobo","like_artist":true,"from_year":2000,"to_year":3000}`})
	// The rules already understand "sounds like ... bonobo", so check the model path directly.
	if it.Source != "rules" {
		t.Fatalf("rules should win: %+v", it)
	}
	in := NewInterpreter(catalog(), &fakeLLM{out: `{"artist":"BONOBO","like_artist":true,"from_year":2000,"to_year":3000,"lane":"bogus"}`}, nov2026)
	it, ok := in.ask(context.Background(), "x", catalog().artists, catalog().genres)
	if !ok || it.Lane != "sounds-like" || it.Seed.ID != "bonobo" || it.FromYear != 2000 || it.ToYear != 2009 {
		t.Fatalf("got %+v", it)
	}
}

func TestModelFailureFallsBackToRules(t *testing.T) {
	for _, m := range []*fakeLLM{{err: errors.New("down")}, {out: "I can't help with that"}, {out: "{not json"}} {
		it := interpret(t, "something to cook to", m)
		if it.Understood || it.Source != "rules" {
			t.Errorf("%+v: got %+v", m, it)
		}
	}
}

func TestInterpretCatalogError(t *testing.T) {
	cat := catalog()
	cat.err = errors.New("db gone")
	if _, err := NewInterpreter(cat, nil, nov2026).Interpret(context.Background(), "bonobo"); err == nil {
		t.Fatal("want error")
	}
}

func TestCachedCatalog(t *testing.T) {
	now := time.Unix(1000, 0)
	src := catalog()
	c := NewCachedCatalog(src, time.Minute, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if a, _ := c.Artists(context.Background()); len(a) != 5 {
			t.Fatal(a)
		}
	}
	if src.calls != 1 {
		t.Fatalf("calls %d", src.calls)
	}
	now = now.Add(2 * time.Minute)
	_, _ = c.Genres(context.Background())
	if src.calls != 2 {
		t.Fatalf("calls %d", src.calls)
	}
}

type fakeLister struct{}

func (fakeLister) ListLibraryArtistKeys(context.Context) ([]db.ListLibraryArtistKeysRow, error) {
	return []db.ListLibraryArtistKeysRow{{ArtistKey: "bonobo", Name: "Bonobo", Tracks: 3}}, nil
}
func (fakeLister) ListLibraryGenres(context.Context, int64) ([]db.ListLibraryGenresRow, error) {
	return []db.ListLibraryGenresRow{{GenreKey: "électro", Tracks: 2}}, nil
}

func TestLibraryCatalog(t *testing.T) {
	c := NewLibraryCatalog(fakeLister{})
	a, _ := c.Artists(context.Background())
	g, _ := c.Genres(context.Background())
	if len(a) != 1 || a[0] != (Name{Key: "bonobo", Name: "Bonobo"}) || len(g) != 1 || g[0].Name != "Électro" {
		t.Fatalf("%+v %+v", a, g)
	}
}
