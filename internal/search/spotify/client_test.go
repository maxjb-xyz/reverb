package spotify

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestTokenBasicAuthAndForm(t *testing.T) {
	var gotAuth, gotGrant, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		b, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.URL+"/v1", "cid", "csecret", srv.Client())
	tok, err := c.token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "BQDtESTtoken123" {
		t.Fatalf("token = %q", tok)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:csecret"))
	if gotAuth != want {
		t.Fatalf("auth = %q, want %q", gotAuth, want)
	}
	if gotGrant != "client_credentials" {
		t.Fatalf("grant_type = %q", gotGrant)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("content-type = %q", gotCT)
	}
}

func TestTokenIsCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		b, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.URL+"/v1", "cid", "csecret", srv.Client())
	for i := 0; i < 3; i++ {
		if _, err := c.token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("token endpoint hit %d times, want 1 (cached)", hits)
	}
}

func TestApiGetSendsBearer(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, srv.URL+"/v1", "cid", "csecret", srv.Client())
	var out map[string]any
	if err := c.apiGet(context.Background(), "/ping", url.Values{}, &out); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer BQDtESTtoken123" {
		t.Fatalf("authorization = %q", gotAuth)
	}
}
