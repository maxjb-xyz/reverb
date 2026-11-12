package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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
