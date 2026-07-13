package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// Matcher pre-matches an external result against the library. Implemented by
// matching.Service (Task 9). A nil Matcher leaves Match unset.
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

// FindTrack finds a track in the configured sources when a durable catalog row
// originated in the local library and therefore lacks an external identity.
// Sources are consulted in configured priority order; an exact title/artist
// match is required so stats links never point at a merely similar song.
func (a *Aggregator) FindTrack(ctx context.Context, title, artist string) (core.ExternalResult, error) {
	for _, src := range a.sources {
		results, err := src.Search(ctx, title, core.EntityTrack)
		if err != nil {
			continue
		}
		for _, result := range results {
			if result.Title == title && result.Artist == artist {
				return result, nil
			}
		}
	}
	return core.ExternalResult{}, fmt.Errorf("no configured source matched %q by %q", title, artist)
}

// Aggregator fans out a query to every enabled SearchSource concurrently.
type Aggregator struct {
	sources []SearchSource
	matcher Matcher
	timeout time.Duration
}

// NewAggregator builds an aggregator. timeout is the per-source deadline.
func NewAggregator(sources []SearchSource, matcher Matcher, timeout time.Duration) *Aggregator {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Aggregator{sources: sources, matcher: matcher, timeout: timeout}
}

func (a *Aggregator) source(name string) SearchSource {
	for _, src := range a.sources {
		if src.Name() == name {
			return src
		}
	}
	return nil
}

// GetTrack resolves one durable source track without fuzzy searching.
func (a *Aggregator) GetTrack(ctx context.Context, source, id string) (core.ExternalResult, error) {
	src := a.source(source)
	p, ok := src.(TrackProvider)
	if !ok {
		return core.ExternalResult{}, fmt.Errorf("search source %q does not support track lookup", source)
	}
	return p.GetTrack(ctx, id)
}

func (a *Aggregator) GetArtist(ctx context.Context, source, id string) (core.ExternalArtist, error) {
	src := a.source(source)
	p, ok := src.(ArtistProvider)
	if !ok {
		return core.ExternalArtist{}, fmt.Errorf("search source %q does not support artist lookup", source)
	}
	return p.GetArtist(ctx, id)
}

func (a *Aggregator) GetAlbum(ctx context.Context, source, id string) (core.ExternalAlbum, error) {
	src := a.source(source)
	if src == nil {
		return core.ExternalAlbum{}, fmt.Errorf("search source %q not configured", source)
	}
	return src.GetAlbum(ctx, id)
}

// Stream runs each source in its own goroutine with an individual
// context.WithTimeout, pre-matches each result, and emits one Envelope per
// source. The channel is closed once every source completes (so an SSE handler
// ranging over it returns). A slow/down source never blocks the others.
func (a *Aggregator) Stream(ctx context.Context, q string, t core.EntityType) <-chan Envelope {
	out := make(chan Envelope)
	var wg sync.WaitGroup
	for _, src := range a.sources {
		wg.Add(1)
		go func(src SearchSource) {
			defer wg.Done()
			env := a.runOne(ctx, src, q, t)
			select {
			case out <- env:
			case <-ctx.Done():
			}
		}(src)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func (a *Aggregator) runOne(ctx context.Context, src SearchSource, q string, t core.EntityType) Envelope {
	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	results, err := src.Search(cctx, q, t)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return Envelope{Source: src.Name(), Status: StatusTimeout, Results: []core.ExternalResult{}}
		}
		return Envelope{Source: src.Name(), Status: StatusError, Results: []core.ExternalResult{}, Error: err.Error()}
	}
	// Playlist discovery augments the normal track search only when a provider
	// explicitly supports it. A playlist failure must not discard useful tracks.
	if t == core.EntityTrack {
		if provider, ok := src.(PlaylistSearchProvider); ok {
			if playlists, perr := provider.SearchPlaylists(cctx, q); perr == nil {
				results = append(results, playlists...)
			}
		}
	}

	if a.matcher != nil {
		for i := range results {
			m, merr := a.matcher.Match(cctx, results[i])
			if merr == nil {
				mc := m
				results[i].Match = &mc
			} else {
				results[i].Match = &core.MatchResult{Status: core.MatchUnknown, Method: core.MatchNone}
			}
		}
	}
	if results == nil {
		results = []core.ExternalResult{}
	}
	return Envelope{Source: src.Name(), Status: StatusOK, Results: results}
}
