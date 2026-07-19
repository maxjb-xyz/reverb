package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/library/lyrics"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// fakeLyrics implements the LyricsProvider seam.
type fakeLyrics struct {
	got lyrics.Request
	res lyrics.Lyrics
	ok  bool
}

func (f *fakeLyrics) Get(_ context.Context, req lyrics.Request) (lyrics.Lyrics, bool, error) {
	f.got = req
	return f.res, f.ok, nil
}

// lyricsTestServer builds a Server with a temp store, an authed session, and
// the given LyricsProvider wired in.
func lyricsTestServer(t *testing.T, lp LyricsProvider) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/lyrics.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Lyrics:     lp,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestTrackLyrics_SyncedPayload(t *testing.T) {
	fake := &fakeLyrics{
		res: lyrics.Lyrics{Synced: true, Lines: []lyrics.Line{{TimeMs: 1000, Text: "Hi"}}},
		ok:  true,
	}
	srv, cookie := lyricsTestServer(t, fake)

	rec := doGET(t, srv, "/api/v1/library/track/t1/lyrics?artist=A&title=T&album=L&durationMs=180000", cookie.Value)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	want := `{"lines":[{"timeMs":1000,"text":"Hi"}],"synced":true}`
	if got := rec.Body.String(); got != want+"\n" && got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if fake.got.TrackID != "t1" {
		t.Fatalf("TrackID = %q, want t1", fake.got.TrackID)
	}
	wantQuery := lyrics.Query{Artist: "A", Title: "T", Album: "L", DurationMs: 180000}
	if fake.got.Query != wantQuery {
		t.Fatalf("Query = %+v, want %+v", fake.got.Query, wantQuery)
	}
}

func TestTrackLyrics_NoLyricsIs204(t *testing.T) {
	fake := &fakeLyrics{ok: false}
	srv, cookie := lyricsTestServer(t, fake)

	rec := doGET(t, srv, "/api/v1/library/track/t1/lyrics?artist=A&title=T&album=L&durationMs=180000", cookie.Value)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

func TestTrackLyrics_NilServiceIs204(t *testing.T) {
	srv, cookie := lyricsTestServer(t, nil)

	rec := doGET(t, srv, "/api/v1/library/track/t1/lyrics?artist=A&title=T&album=L&durationMs=180000", cookie.Value)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}
