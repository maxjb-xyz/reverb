package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/engine"
	"github.com/maxjb-xyz/reverb/internal/taste"

	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func engineTestServer(t *testing.T, withIndex bool) (*Server, *http.Cookie, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/engine.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	deps := Deps{Auth: authSvc, Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")}
	if withIndex {
		deps.LibIndex = libindex.New(st.DB(), func() (library.LibraryAdapter, []search.SearchSource) { return nil, nil }, time.Now, libindex.Options{})
	}
	return NewServer(deps), &http.Cookie{Name: sessionCookie, Value: tok}, st
}

func TestSimilarArtists_ReturnsStoredMapWithOwnership(t *testing.T) {
	srv, cookie, st := engineTestServer(t, true)
	ctx := context.Background()
	q := st.Q()
	if err := q.UpsertLibraryTrack(ctx, db.UpsertLibraryTrackParams{LibraryTrackID: "t1", Title: "x", Artist: "Justice", ArtistKey: "justice", ArtistID: "ar-j", Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	for i, n := range []string{"Justice", "Air"} {
		if err := q.InsertArtistSimilar(ctx, db.InsertArtistSimilarParams{ArtistKey: "daft punk", Source: "deezer", SimilarKey: map[string]string{"Justice": "justice", "Air": "air"}[n], SimilarName: n, Rank: int64(i), FetchedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/artists/similar?name=Daft%20Punk", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body.String())
	}
	var body similarArtistsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Similar) != 2 || body.Similar[0].LibraryArtistID != "ar-j" || body.Similar[0].OwnedTracks != 1 || body.Similar[1].OwnedTracks != 0 {
		t.Fatalf("similar = %+v", body.Similar)
	}
}

func TestSimilarArtists_RequiresName(t *testing.T) {
	srv, cookie, _ := engineTestServer(t, true)
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/artists/similar", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestIndexEndpoints(t *testing.T) {
	srv, cookie, _ := engineTestServer(t, true)
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/engine/index", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var st libindex.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Tracks != 0 {
		t.Fatalf("status body = %s (%v)", rec.Body.String(), err)
	}
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/engine/index/rebuild", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("rebuild = %d", rec.Code)
	}
}

func TestIndexEndpoints_NilServiceIs503(t *testing.T) {
	srv, cookie, _ := engineTestServer(t, false)
	for _, p := range []string{"/api/v1/engine/index", "/api/v1/artists/similar?name=x"} {
		if rec := do(t, srv, cookie, http.MethodGet, p, ""); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s = %d, want 503", p, rec.Code)
		}
	}
}

func engineNextServer(t *testing.T, withEngine bool) (*Server, *http.Cookie, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/engine.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	deps := Deps{Auth: authSvc, Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")}
	if withEngine {
		n := 0
		idgen := func() string { n++; return fmt.Sprintf("id-%d", n) }
		cat := catalog.NewService(st.Q(), time.Now, idgen)
		tasteSvc := taste.NewService(st.Q(), cat, time.Now, idgen)
		idx := libindex.New(st.DB(), func() (library.LibraryAdapter, []search.SearchSource) { return nil, nil }, time.Now, libindex.Options{})
		deps.Engine = engine.New(st.Q(), tasteSvc, idx, nil, time.Now, nil, engine.DefaultOptions)
	}
	return NewServer(deps), &http.Cookie{Name: sessionCookie, Value: tok}, st
}

func TestEngineNext_ArtistRadioAndErrors(t *testing.T) {
	srv, cookie, st := engineNextServer(t, true)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		artist := []string{"Bonobo", "Tycho"}[i%2]
		if err := st.Q().UpsertLibraryTrack(ctx, db.UpsertLibraryTrackParams{
			LibraryTrackID: fmt.Sprintf("t%d", i), Title: fmt.Sprintf("Song %d", i), Artist: artist, ArtistKey: strings.ToLower(artist),
			AlbumID: fmt.Sprintf("al%d", i), DurationMs: 200_000, Epoch: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/engine/next", `{"seed":{"kind":"artist","id":"bonobo"},"exclude":["t0"],"n":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body.String())
	}
	var res engine.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Station.ID != "artist:bonobo" || len(res.Items) == 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, c := range res.Items {
		if c.Track == nil || c.Track.ID == "t0" || c.Reason.Kind == "" {
			t.Fatalf("item = %+v", c)
		}
	}
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/engine/next", `{"lane":"vibes"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad lane = %d", rec.Code)
	}
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/engine/next", `{"seed":{"kind":"track","id":"missing"}}`); rec.Code != http.StatusNotFound {
		t.Errorf("missing seed = %d", rec.Code)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/engine/stations", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"stations"`) {
		t.Errorf("stations = %d %s", rec.Code, rec.Body.String())
	}
}

func TestEngineNext_UnavailableAndUnauthenticated(t *testing.T) {
	srv, cookie, _ := engineNextServer(t, false)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/engine/next", `{}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no engine = %d", rec.Code)
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/engine/stations", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no engine stations = %d", rec.Code)
	}
	if rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: "bogus"}, http.MethodPost, "/api/v1/engine/next", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d", rec.Code)
	}
}
