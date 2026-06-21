package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store"
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
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(context.Background())

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
