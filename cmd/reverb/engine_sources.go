package main

import (
	"context"
	"time"

	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// engineSources is what the recommendation engine reads: the live search
// sources, plus fallback (Deezer, which needs no account) when it is switched
// on and none of them can list similar artists. Without it a server whose
// only search source is Spotify has no similar artists, no new songs for
// Discover Weekly and no "Fans also like".
func engineSources(live func() (library.LibraryAdapter, []search.SearchSource), fallback search.SearchSource, on func() bool) func() (library.LibraryAdapter, []search.SearchSource) {
	return func() (library.LibraryAdapter, []search.SearchSource) {
		lib, sources := live()
		for _, src := range sources {
			if _, ok := src.(search.SimilarArtistsProvider); ok {
				return lib, sources
			}
		}
		if fallback == nil || !on() {
			return lib, sources
		}
		out := make([]search.SearchSource, 0, len(sources)+1)
		out = append(out, sources...)
		return lib, append(out, fallback)
	}
}

// engineDeezerOn reads the admin switch; it is on unless set to "off".
func engineDeezerOn(get func(ctx context.Context, key string) (string, error)) func() bool {
	return func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		v, err := get(ctx, libindex.SettingDeezer)
		return err != nil || v != "off"
	}
}
