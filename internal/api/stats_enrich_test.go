package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// slowMetadata is a search source stand-in whose every lookup takes `delay`,
// like a real round trip to Deezer or Spotify.
type slowMetadata struct {
	delay    time.Duration
	inFlight atomic.Int32
	peak     atomic.Int32
}

func (f *slowMetadata) wait() {
	n := f.inFlight.Add(1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(f.delay)
	f.inFlight.Add(-1)
}

func (f *slowMetadata) Stream(context.Context, string, core.EntityType) <-chan search.Envelope {
	ch := make(chan search.Envelope)
	close(ch)
	return ch
}

func (f *slowMetadata) GetTrack(_ context.Context, source, id string) (core.ExternalResult, error) {
	f.wait()
	return core.ExternalResult{}, fmt.Errorf("not found")
}

func (f *slowMetadata) GetArtist(_ context.Context, source, id string) (core.ExternalArtist, error) {
	f.wait()
	return core.ExternalArtist{CoverURL: "https://img/artist/" + id}, nil
}

func (f *slowMetadata) GetAlbum(_ context.Context, source, id string) (core.ExternalAlbum, error) {
	f.wait()
	return core.ExternalAlbum{}, nil
}

func (f *slowMetadata) FindTrack(_ context.Context, title, artist string) (core.ExternalResult, error) {
	f.wait()
	return core.ExternalResult{
		Source: "deezer", ExternalID: "t-" + title, ArtistExternalID: "a-" + artist,
		CoverURL: "https://img/track/" + title,
	}, nil
}

// Artwork lookups for the top lists run side by side. One after another, ten
// artists cost twenty round trips before the list could show.
func TestEnrichStatsRows_LooksUpRowsConcurrently(t *testing.T) {
	meta := &slowMetadata{delay: 100 * time.Millisecond}
	srv := NewServer(Deps{SearchAggregator: meta})

	rows := make([]play.TopRow, 10)
	for i := range rows {
		rows[i] = play.TopRow{Title: fmt.Sprintf("Song %d", i), Artist: fmt.Sprintf("Artist %d", i)}
	}

	start := time.Now()
	srv.enrichStatsRows(context.Background(), rows, "artist")
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("enriching 10 rows took %v; lookups are not running concurrently", elapsed)
	}
	if meta.peak.Load() < 2 {
		t.Fatalf("peak concurrent lookups = %d, want > 1", meta.peak.Load())
	}
	if meta.peak.Load() > statsEnrichConcurrency {
		t.Fatalf("peak concurrent lookups = %d, want at most %d", meta.peak.Load(), statsEnrichConcurrency)
	}
	for i, r := range rows {
		if want := fmt.Sprintf("https://img/artist/a-Artist %d", i); r.CoverURL != want {
			t.Fatalf("row %d CoverURL = %q, want %q", i, r.CoverURL, want)
		}
	}
}

// TestStatsTimelineLocalDays: the timeline handler passes the browser's time
// zone through, so a late-evening play lands on the listener's own day.
func TestStatsTimelineLocalDays(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)
	jan2 := int64(946771200) // 2000-01-02 00:00 UTC
	seedPlay(t, playSvc, ownerID, "T1", "A", "Alb", jan2+3*3600, 100_000)

	rec := doGET(t, srv, "/api/v1/stats/timeline?from=0&to=9999999999&bucket=day&tzOffsetMinutes=-300", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/timeline = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Local midnight of Jan 1 at UTC-5 is 05:00 UTC on Jan 1.
	want := fmt.Sprintf(`"Start":%d`, jan2-86400+5*3600)
	if !strings.Contains(body, want) {
		t.Fatalf("timeline %s does not contain %s", body, want)
	}
}
