package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

func tasteTestServer(t *testing.T) (*Server, *http.Cookie, string, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/taste.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	users, err := authSvc.ListUsers(context.Background())
	if err != nil || len(users) == 0 {
		t.Fatalf("users: %v %v", users, err)
	}
	n := 0
	idgen := func() string { n++; return fmt.Sprintf("%08d-0000-0000-0000-000000000000", n) }
	cat := catalog.NewService(st.Q(), time.Now, idgen)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Taste:      taste.NewService(st.Q(), cat, time.Now, idgen),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}, users[0].ID, st
}

func TestHandleListen_RecordsOutcomeAndContext(t *testing.T) {
	srv, cookie, owner, st := tasteTestServer(t)
	body := `{"libraryTrackId":"lib-1","title":"Hurt","artist":"Johnny Cash","album":"American IV",
		"durationMs":218000,"msPlayed":4000,"outcome":"skip_early","contextKind":"radio","contextId":"st-9"}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/listens", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /listens = %d: %s", rec.Code, rec.Body.String())
	}
	rows, err := st.Q().ListListenEventsSince(context.Background(), db.ListListenEventsSinceParams{UserID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 listen for the session user, got %d", len(rows))
	}
	r := rows[0]
	if r.Outcome != "skip_early" || r.ContextKind != "radio" || r.ContextID != "st-9" || r.LibraryTrackID != "lib-1" || r.ArtistKey != taste.ArtistKey("Johnny Cash") {
		t.Fatalf("stored listen = %+v", r)
	}
}

func TestHandleListen_RejectsUnknownOutcome(t *testing.T) {
	srv, cookie, _, _ := tasteTestServer(t)
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/listens", `{"title":"x","artist":"y","outcome":"loved"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestTasteEndpoints_Unauthenticated401(t *testing.T) {
	srv, _, _, _ := tasteTestServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/listens"},
		{http.MethodGet, "/api/v1/taste/profile"},
		{http.MethodGet, "/api/v1/taste/feedback"},
		{http.MethodPut, "/api/v1/taste/feedback"},
		{http.MethodDelete, "/api/v1/taste/feedback/track/x"},
	} {
		rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: "bogus"}, c.method, c.path, `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestTasteEndpoints_NilServiceIs503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/x.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{Auth: authSvc, Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")})
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: tok}, http.MethodPost, "/api/v1/listens", `{"title":"x","outcome":"completed"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestTasteFeedback_SetListClearAndProfile(t *testing.T) {
	srv, cookie, _, _ := tasteTestServer(t)

	// Listen to two artists so the profile has content.
	for _, b := range []string{
		`{"title":"Hurt","artist":"Johnny Cash","album":"A","durationMs":218000,"msPlayed":218000,"outcome":"completed"}`,
		`{"title":"Song 2","artist":"Blur","album":"B","durationMs":122000,"msPlayed":122000,"outcome":"completed"}`,
	} {
		if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/listens", b); rec.Code != http.StatusNoContent {
			t.Fatalf("listen: %d %s", rec.Code, rec.Body.String())
		}
	}

	rec := do(t, srv, cookie, http.MethodPut, "/api/v1/taste/feedback", `{"targetKind":"artist","artist":"Sigur Rós","libraryArtistId":"ar-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT artist feedback = %d: %s", rec.Code, rec.Body.String())
	}
	var art taste.Feedback
	_ = json.Unmarshal(rec.Body.Bytes(), &art)
	if art.Label != "Sigur Rós" || art.TargetKey == "" {
		t.Fatalf("artist feedback = %+v", art)
	}
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/taste/feedback", `{"targetKind":"track","title":"Song 2","artist":"Blur","album":"B","durationMs":122000,"libraryTrackId":"lib-2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT track feedback = %d: %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/taste/feedback", "")
	var list []taste.Feedback
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("GET feedback = %s (%v)", rec.Body.String(), err)
	}

	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/taste/profile", "")
	var prof tasteProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &prof); err != nil {
		t.Fatal(err)
	}
	if len(prof.TopTracks) != 1 || prof.TopTracks[0].Title != "Hurt" || prof.HiddenTracks != 1 || prof.HiddenArtists != 1 {
		t.Fatalf("profile = %+v; want only Hurt on top (Song 2 hidden)", prof)
	}

	// The artist key contains a non-ASCII char; the client percent-encodes it.
	rec = do(t, srv, cookie, http.MethodDelete, "/api/v1/taste/feedback/artist/"+url.PathEscape(art.TargetKey), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/taste/feedback", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].TargetKind != "track" {
		t.Fatalf("after delete = %+v; want only the track row", list)
	}
}

func TestTasteSignal_LibraryHelperKeysLikePlays(t *testing.T) {
	ref := tasteRefFor("library", "lib-7", "T", "A", "B", "", 1000)
	if ref.Source != "" || ref.ExternalID != "" || ref.LibraryTrackID != "lib-7" {
		t.Fatalf("library ref = %+v; must be metadata-only like play.Record", ref)
	}
	ref = tasteRefFor("deezer", "123", "T", "A", "B", "ISRC1", 1000)
	if ref.Source != "deezer" || ref.ExternalID != "123" {
		t.Fatalf("external ref = %+v", ref)
	}
	ref = tasteRefFor("deezer", "alb-1", "", "A", "B", "", 0)
	if ref.Source != "" || ref.Title != "" {
		t.Fatalf("artist-level ref must not carry a source id: %+v", ref)
	}
}

func TestAddSyncedTrack_RecordsPlaylistAddSignal(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/sig.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	users, _ := authSvc.ListUsers(context.Background())
	n := 0
	idgen := func() string { n++; return fmt.Sprintf("%08d-0000-0000-0000-000000000000", n) }
	cat := catalog.NewService(st.Q(), time.Now, idgen)
	tasteSvc := taste.NewService(st.Q(), cat, time.Now, idgen)
	svc := &fakeSync{detail: core.SyncedPlaylistDetail{SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"}}}
	srv := NewServer(Deps{
		Auth: authSvc, Sync: svc, PlaylistOwner: st.Q(), Taste: tasteSvc,
		Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader"),
	})
	body := `{"source":"library","externalId":"lib-5","title":"Hurt","artist":"Johnny Cash","album":"American IV","durationMs":218000}`
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: tok}, http.MethodPost, "/api/v1/playlists/pl-1/tracks", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("add track = %d: %s", rec.Code, rec.Body.String())
	}
	p, err := tasteSvc.Profile(context.Background(), users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ArtistScore(taste.ArtistKey("Johnny Cash")) < 2 {
		t.Fatalf("playlist add should lift the artist, profile = %+v", p.Artists)
	}
}
