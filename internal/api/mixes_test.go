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
	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/mixes"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

func mixesTestServer(t *testing.T, with bool) (*Server, *http.Cookie, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/mixes.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	deps := Deps{Auth: authSvc, Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")}
	if with {
		n := 0
		idgen := func() string { n++; return fmt.Sprintf("id-%d", n) }
		cat := catalog.NewService(st.Q(), time.Now, idgen)
		tasteSvc := taste.NewService(st.Q(), cat, time.Now, idgen)
		idx := libindex.New(st.DB(), func() (library.LibraryAdapter, []search.SearchSource) { return nil, nil }, time.Now, libindex.Options{})
		eng := engine.New(st.Q(), tasteSvc, idx, nil, time.Now, nil, engine.DefaultOptions)
		deps.Mixes = mixes.New(st.Q(), eng, tasteSvc, idx, time.Now, idgen)
	}
	return NewServer(deps), &http.Cookie{Name: sessionCookie, Value: tok}, st
}

func TestMixesAPI_CustomMixLifecycle(t *testing.T) {
	srv, cookie, st := mixesTestServer(t, true)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		if err := st.Q().UpsertLibraryTrack(ctx, db.UpsertLibraryTrackParams{
			LibraryTrackID: fmt.Sprintf("t%d", i), Title: fmt.Sprintf("Song %d", i), Artist: "Bonobo", ArtistKey: "bonobo",
			AlbumID: fmt.Sprintf("al%d", i), DurationMs: 200_000, Epoch: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/mixes", `{"name":"Chill","recipe":{"seeds":[{"kind":"artist","id":"bonobo"}],"size":5,"refresh":"manual"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var m mixes.Mix
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "Chill" || len(m.Items) != 5 || m.Kind != "custom" {
		t.Fatalf("mix = %+v", m)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/mixes", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Chill"`) {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/mixes/"+m.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	if rec := do(t, srv, cookie, http.MethodPut, "/api/v1/mixes/"+m.ID, `{"name":"Chiller","recipe":{"seeds":[{"kind":"artist","id":"bonobo"}],"size":3}}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Chiller") {
		t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/mixes/"+m.ID+"/refresh", ""); rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d", rec.Code)
	}
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/mixes", `{"name":"Bad","recipe":{"lane":"vibes"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad recipe = %d", rec.Code)
	}
	if rec := do(t, srv, cookie, http.MethodDelete, "/api/v1/mixes/"+m.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/mixes/"+m.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted = %d", rec.Code)
	}
}

func TestMixesAPI_UnavailableAndUnauthenticated(t *testing.T) {
	srv, cookie, _ := mixesTestServer(t, false)
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/mixes", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no service = %d", rec.Code)
	}
	srv, _, _ = mixesTestServer(t, true)
	if rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: "bogus"}, http.MethodGet, "/api/v1/mixes", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d", rec.Code)
	}
}

func TestMixesAPI_RebuildDaily(t *testing.T) {
	srv, cookie, _ := mixesTestServer(t, true)
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/mixes/daily/rebuild", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mixes":`) {
		t.Fatalf("rebuild = %d %s", rec.Code, rec.Body.String())
	}
}
