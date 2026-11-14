package main

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// plainSrc is a search source with no similar-artist capability (Spotify-like).
type plainSrc struct {
	search.SearchSource
	name string
}

func (p plainSrc) Name() string { return p.name }

// simSrc can list similar artists (Deezer-like).
type simSrc struct{ plainSrc }

func (simSrc) SimilarArtists(context.Context, string, int) ([]core.ExternalArtist, error) {
	return nil, nil
}

func names(srcs []search.SearchSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.Name())
	}
	return out
}

func TestEngineSources_AddsDeezerWhenNoSourceFindsSimilarArtists(t *testing.T) {
	fallback := simSrc{plainSrc{name: "deezer"}}
	cases := []struct {
		name string
		live []search.SearchSource
		on   bool
		want []string
	}{
		{"only spotify", []search.SearchSource{plainSrc{name: "spotify"}}, true, []string{"spotify", "deezer"}},
		{"no sources", nil, true, []string{"deezer"}},
		{"switched off", []search.SearchSource{plainSrc{name: "spotify"}}, false, []string{"spotify"}},
		{"deezer search already on", []search.SearchSource{simSrc{plainSrc{name: "deezer"}}}, true, []string{"deezer"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			live := func() (library.LibraryAdapter, []search.SearchSource) { return nil, c.live }
			_, got := engineSources(live, fallback, func() bool { return c.on })()
			if g := names(got); len(g) != len(c.want) || (len(g) > 0 && (g[0] != c.want[0] || g[len(g)-1] != c.want[len(c.want)-1])) {
				t.Fatalf("sources = %v, want %v", g, c.want)
			}
		})
	}
}

func TestEngineDeezerOn_DefaultsOnUntilSwitchedOff(t *testing.T) {
	vals := map[string]string{}
	get := func(_ context.Context, k string) (string, error) { return vals[k], nil }
	on := engineDeezerOn(get)
	if !on() {
		t.Fatal("Deezer for recommendations should be on by default")
	}
	vals["engine_deezer"] = "off"
	if on() {
		t.Fatal("switched off but still on")
	}
}
