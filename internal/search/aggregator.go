package search

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/maximusjb/crate/internal/core"
)

// Matcher pre-matches an external result against the library. Implemented by
// matching.Service (Task 9). A nil Matcher leaves Match unset.
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
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
			out <- a.runOne(ctx, src, q, t)
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
