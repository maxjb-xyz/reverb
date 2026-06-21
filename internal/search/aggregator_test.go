package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// scriptedSource returns canned results after an artificial delay.
type scriptedSource struct {
	name    string
	delay   time.Duration
	results []core.ExternalResult
	err     error
}

func (s *scriptedSource) Type() string                         { return "search" }
func (s *scriptedSource) Name() string                         { return s.name }
func (s *scriptedSource) ConfigSchema() registry.ConfigSchema  { return registry.ConfigSchema{} }
func (s *scriptedSource) Init(map[string]any) error            { return nil }
func (s *scriptedSource) TestConnection(context.Context) error { return nil }
func (s *scriptedSource) GetAlbum(context.Context, string) (core.ExternalAlbum, error) {
	return core.ExternalAlbum{}, nil
}
func (s *scriptedSource) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	select {
	case <-time.After(s.delay):
		return s.results, s.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fakeMatcher marks any result with a non-empty ISRC as in_library.
type fakeMatcher struct{}

func (fakeMatcher) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	if ext.ISRC != "" {
		return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: "lib-" + ext.ExternalID, Method: core.MatchISRC, Confidence: 1}, nil
	}
	return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone}, nil
}

func collect(ch <-chan Envelope) map[string]Envelope {
	out := map[string]Envelope{}
	for e := range ch {
		out[e.Source] = e
	}
	return out
}

func TestAggregatorPreMatchesAndEmitsPerSource(t *testing.T) {
	fast := &scriptedSource{name: "fast", delay: 0, results: []core.ExternalResult{
		{Source: "fast", ExternalID: "f1", Title: "A", ISRC: "USX1"},
		{Source: "fast", ExternalID: "f2", Title: "B"},
	}}
	other := &scriptedSource{name: "other", delay: 0, results: []core.ExternalResult{
		{Source: "other", ExternalID: "o1", Title: "C"},
	}}
	agg := NewAggregator([]SearchSource{fast, other}, fakeMatcher{}, time.Second)
	got := collect(agg.Stream(context.Background(), "q", core.EntityTrack))

	if len(got) != 2 {
		t.Fatalf("want 2 envelopes, got %d: %+v", len(got), got)
	}
	if got["fast"].Status != StatusOK || len(got["fast"].Results) != 2 {
		t.Fatalf("fast envelope wrong: %+v", got["fast"])
	}
	if got["fast"].Results[0].Match == nil || got["fast"].Results[0].Match.Status != core.MatchInLibrary {
		t.Fatalf("ISRC result not pre-matched: %+v", got["fast"].Results[0])
	}
	if got["fast"].Results[1].Match == nil || got["fast"].Results[1].Match.Status != core.MatchNotInLibrary {
		t.Fatalf("non-ISRC result not pre-matched: %+v", got["fast"].Results[1])
	}
}

func TestAggregatorTimeoutDoesNotBlockOthers(t *testing.T) {
	start := time.Now()
	slow := &scriptedSource{name: "slow", delay: 200 * time.Millisecond, results: []core.ExternalResult{{Source: "slow", ExternalID: "s1"}}}
	fast := &scriptedSource{name: "fast", delay: 0, results: []core.ExternalResult{{Source: "fast", ExternalID: "f1"}}}
	agg := NewAggregator([]SearchSource{slow, fast}, fakeMatcher{}, 20*time.Millisecond)
	got := collect(agg.Stream(context.Background(), "q", core.EntityTrack))

	if got["fast"].Status != StatusOK {
		t.Fatalf("fast should be ok, got %+v", got["fast"])
	}
	if got["slow"].Status != StatusTimeout {
		t.Fatalf("slow should be timeout, got %+v", got["slow"])
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("Stream took %v; want < 100ms (slow source delay is 200ms — blocking regression)", elapsed)
	}
}

func TestAggregatorErrorEnvelope(t *testing.T) {
	bad := &scriptedSource{name: "bad", delay: 0, err: errors.New("boom")}
	agg := NewAggregator([]SearchSource{bad}, fakeMatcher{}, time.Second)
	got := collect(agg.Stream(context.Background(), "q", core.EntityTrack))
	if got["bad"].Status != StatusError || got["bad"].Error == "" {
		t.Fatalf("want error envelope, got %+v", got["bad"])
	}
}

// TestAggregatorCancelExitsCleanly verifies that cancelling the parent context
// causes the output channel to close promptly even when sources are still
// blocked. Without the ctx.Done() escape in the send select this test would
// deadlock (the goroutines block on out<- forever, wg.Wait() never returns,
// the closer goroutine never closes out, and the drain goroutine below times
// out).
func TestAggregatorCancelExitsCleanly(t *testing.T) {
	// slow source takes much longer than our cancel window.
	slow := &scriptedSource{name: "slow", delay: 2 * time.Second, results: []core.ExternalResult{{Source: "slow", ExternalID: "s1"}}}

	ctx, cancel := context.WithCancel(context.Background())
	agg := NewAggregator([]SearchSource{slow}, fakeMatcher{}, 5*time.Second)
	ch := agg.Stream(ctx, "q", core.EntityTrack)

	// Cancel immediately — the slow source is still blocked in its delay.
	cancel()

	// Drain the channel in a goroutine so we can apply a timeout.
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
		// channel closed promptly — no leak.
	case <-time.After(time.Second):
		t.Fatal("output channel did not close within 1s after context cancel — goroutine leak")
	}
}

func TestAggregatorChannelClosesWithNoSources(t *testing.T) {
	agg := NewAggregator(nil, fakeMatcher{}, time.Second)
	// Range terminates only if the channel is closed.
	for range agg.Stream(context.Background(), "q", core.EntityTrack) {
		t.Fatal("expected no envelopes")
	}
}
