package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/discover"
	"github.com/maxjb-xyz/reverb/internal/engine"
	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

func discoverTestServer(t *testing.T, with bool) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/discover.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	deps := Deps{Auth: authSvc, Adapters: st.Q(), Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")}
	if with {
		n := 0
		idgen := func() string { n++; return fmt.Sprintf("id-%d", n) }
		cat := catalog.NewService(st.Q(), time.Now, idgen)
		tasteSvc := taste.NewService(st.Q(), cat, time.Now, idgen)
		idx := libindex.New(st.DB(), func() (library.LibraryAdapter, []search.SearchSource) { return nil, nil }, time.Now, libindex.Options{})
		eng := engine.New(st.Q(), tasteSvc, idx, nil, time.Now, nil, engine.DefaultOptions)
		deps.Discover = discover.New(st.Q(), eng, discover.Options{Remove: func(string) error { return nil }}, time.Now, idgen)
	}
	return NewServer(deps), &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestDiscoverAPI_WeekPrefsAndSettings(t *testing.T) {
	srv, cookie := discoverTestServer(t, true)
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/discover", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"week":"`) || !strings.Contains(rec.Body.String(), `"keepRule":"playlist"`) {
		t.Fatalf("week = %d %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/discover/prefs", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) || !strings.Contains(rec.Body.String(), `"size":30`) {
		t.Fatalf("prefs = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/discover/prefs", `{"enabled":true,"size":50,"newShare":80,"keepRule":"played"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"keepRule":"played"`) {
		t.Fatalf("put prefs = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodGet, "/api/v1/discover/prefs", ""); !strings.Contains(rec.Body.String(), `"size":50`) {
		t.Fatalf("prefs not saved: %s", rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodPut, "/api/v1/discover/prefs", `{"enabled":true,"size":50,"newShare":80,"keepRule":"never"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad keep rule = %d", rec.Code)
	}

	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/discover/settings", "")
	if rec.Code != http.StatusOK || rec.Body.String() == "" || !strings.Contains(rec.Body.String(), `"downloads":true`) || !strings.Contains(rec.Body.String(), `"weeklyCap":30`) {
		t.Fatalf("settings = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodPut, "/api/v1/discover/settings", `{"downloads":false,"weeklyCap":12}`); rec.Code != http.StatusOK {
		t.Fatalf("put settings = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/discover/settings", "")
	if !strings.Contains(rec.Body.String(), `"downloads":false`) || !strings.Contains(rec.Body.String(), `"weeklyCap":12`) {
		t.Fatalf("settings not saved: %s", rec.Body.String())
	}
	if rec := do(t, srv, cookie, http.MethodPut, "/api/v1/discover/settings", `{"downloads":true,"weeklyCap":5000}`); rec.Code != http.StatusBadRequest {
		t.Errorf("huge cap = %d", rec.Code)
	}
}

func TestDiscoverAPI_Unavailable(t *testing.T) {
	srv, cookie := discoverTestServer(t, false)
	for _, p := range []string{"/api/v1/discover", "/api/v1/discover/prefs", "/api/v1/discover/settings"} {
		if rec := do(t, srv, cookie, http.MethodGet, p, ""); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
}

func TestDiscoverAPI_Regenerate(t *testing.T) {
	srv, cookie := discoverTestServer(t, true)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/discover/regenerate", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"week":"`) {
		t.Fatalf("regenerate = %d %s", rec.Code, rec.Body.String())
	}
	do(t, srv, cookie, http.MethodPut, "/api/v1/discover/prefs", `{"enabled":false,"size":30,"newShare":70,"keepRule":"playlist"}`)
	if rec := do(t, srv, cookie, http.MethodPost, "/api/v1/discover/regenerate", ""); rec.Code != http.StatusConflict {
		t.Fatalf("regenerate while off = %d, want 409", rec.Code)
	}
}
