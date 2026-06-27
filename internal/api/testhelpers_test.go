package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// testDirty is a minimal ConfigDirty for tests.
type testDirty struct{ b atomic.Bool }

func (d *testDirty) Set()        { d.b.Store(true) }
func (d *testDirty) Dirty() bool { return d.b.Load() }

// fakeAdapter is a controllable registry.Plugin with one Secret field.
type fakeAdapter struct {
	typ     string
	name    string
	testErr error
}

func (a *fakeAdapter) Type() string { return a.typ }
func (a *fakeAdapter) Name() string { return a.name }
func (a *fakeAdapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "URL", Type: "string", Required: true},
		{Key: "token", Label: "Token", Type: "string", Required: true, Secret: true},
	}}
}
func (a *fakeAdapter) Init(map[string]any) error            { return nil }
func (a *fakeAdapter) TestConnection(context.Context) error { return a.testErr }

type adapterServerOpts struct {
	dirty   ConfigDirty
	testErr error // controls the fake search adapter's TestConnection
}

// adapterTestServer builds a Server with a temp store, an authed session, and a
// search registry containing a controllable fake adapter named "fake".
func adapterTestServer(t *testing.T, opts adapterServerOpts) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/adapters.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)

	searchReg := registry.NewRegistry("search")
	searchReg.Register("fake", func() registry.Plugin {
		return &fakeAdapter{typ: "search", name: "fake", testErr: opts.testErr}
	})

	srv := NewServer(Deps{
		Auth:        authSvc,
		Adapters:    st.Q(),
		Search:      searchReg,
		Downloader:  registry.NewRegistry("downloader"),
		Lib:         registry.NewRegistry("library"),
		ConfigDirty: opts.dirty,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

// seededAuthToken seeds the system roles, creates the owner account, and returns
// the auth service plus a valid session token. It is the canonical way for api
// tests to obtain an authenticated session now that auth is user-based.
func seededAuthToken(t *testing.T, st *store.Store) (*auth.Service, string) {
	t.Helper()
	authSvc := auth.NewService(st.Q(), time.Now)
	ctx := context.Background()
	if err := authSvc.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	uid, err := authSvc.SetupOwner(ctx, "owner", "pw")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := authSvc.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return authSvc, tok
}

// newTestServer builds a minimal Server backed by a fresh migrated+seeded store.
// Setup is NOT performed (no owner yet) so first-run flows can be exercised.
func newTestServer(t *testing.T) *Server {
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
	if err := authSvc.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
}

// mustSetupOwner completes first-run setup via POST /setup/admin with {username,password}
// and returns the owner's user ID extracted from the response body.
func mustSetupOwner(t *testing.T, srv *Server, username, password string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(body))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup/admin = %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil || resp.ID == "" {
		t.Fatalf("setup/admin response missing id: %v / body=%s", err, rec.Body.String())
	}
	return resp.ID
}

// mustLogin POSTs /auth/login with {username,password} and returns the session cookie token.
func mustLogin(t *testing.T, srv *Server, username, password string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth/login = %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	t.Fatal("no session cookie set by login")
	return ""
}

// doGET issues a GET with the given session token (empty token → no auth cookie).
func doGET(t *testing.T, srv *Server, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// doPATCH issues a PATCH with an optional session token and a JSON body.
func doPATCH(t *testing.T, srv *Server, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(body))
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// doDELETE issues a DELETE with an optional session token.
func doDELETE(t *testing.T, srv *Server, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// do fires an authenticated HTTP request against the server and returns the recorder.
func do(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = bytes.NewBufferString("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
