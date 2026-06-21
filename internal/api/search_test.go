package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// fakeAgg emits a fixed set of envelopes then closes the channel.
type fakeAgg struct{ envs []search.Envelope }

func (f fakeAgg) Stream(ctx context.Context, q string, t core.EntityType) <-chan search.Envelope {
	ch := make(chan search.Envelope)
	go func() {
		defer close(ch)
		for _, e := range f.envs {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func searchTestServer(t *testing.T, agg Streamer) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, err := authSvc.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{
		Auth:             authSvc,
		SearchAggregator: agg,
		Search:           registry.NewRegistry("search"),
		Downloader:       registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestEverywhereSSEStreamsEnvelopes(t *testing.T) {
	envs := []search.Envelope{
		{Source: "spotify", Status: search.StatusOK, Results: []core.ExternalResult{
			{Source: "spotify", ExternalID: "sp1", Title: "Song", Type: core.EntityTrack,
				Match: &core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: "t1", Method: core.MatchISRC, Confidence: 1}},
		}},
		{Source: "deezer", Status: search.StatusTimeout, Results: []core.ExternalResult{}},
	}
	srv, cookie := searchTestServer(t, fakeAgg{envs: envs})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=song&type=track", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	// Parse "data: <json>" lines.
	var parsed []search.Envelope
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			var e search.Envelope
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
				t.Fatalf("bad event json: %v (%q)", err, line)
			}
			parsed = append(parsed, e)
		}
	}
	if len(parsed) != 2 {
		t.Fatalf("want 2 events, got %d: %s", len(parsed), rec.Body.String())
	}
	if parsed[0].Source != "spotify" || parsed[0].Results[0].Match == nil || parsed[0].Results[0].Match.Status != core.MatchInLibrary {
		t.Fatalf("event0 wrong: %+v", parsed[0])
	}
	if parsed[1].Status != search.StatusTimeout {
		t.Fatalf("event1 should be timeout: %+v", parsed[1])
	}
}

func TestEverywhereRequiresAuth(t *testing.T) {
	srv, _ := searchTestServer(t, fakeAgg{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestEverywhereNilAggregatorReturns503(t *testing.T) {
	srv, cookie := searchTestServer(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=x", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestEverywhereEmptyQueryReturns400(t *testing.T) {
	srv, cookie := searchTestServer(t, fakeAgg{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, should not be an SSE stream", ct)
	}
}
