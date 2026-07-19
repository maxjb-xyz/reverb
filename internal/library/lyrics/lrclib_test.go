package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLRCLib_GetExactMatch(t *testing.T) {
	var gotUA, gotDuration string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotUA = r.Header.Get("User-Agent")
		gotDuration = r.URL.Query().Get("duration")
		w.Write([]byte(`{"syncedLyrics":"[00:01.00]Hi","plainLyrics":"Hi"}`))
	}))
	defer srv.Close()
	c := &LRCLibClient{BaseURL: srv.URL, UserAgent: "Reverb/test"}
	raw, found, err := c.Fetch(context.Background(), Query{Artist: "A", Title: "T", Album: "L", DurationMs: 90_500})
	if err != nil || !found || raw != "[00:01.00]Hi" {
		t.Fatalf("raw=%q found=%v err=%v", raw, found, err)
	}
	if gotUA != "Reverb/test" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	if gotDuration != "91" { // 90.5s rounds to 91
		t.Fatalf("duration = %q, want seconds", gotDuration)
	}
}

func TestLRCLib_PlainOnlyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"syncedLyrics":null,"plainLyrics":"Only plain"}`))
	}))
	defer srv.Close()
	c := &LRCLibClient{BaseURL: srv.URL}
	raw, found, err := c.Fetch(context.Background(), Query{Artist: "A", Title: "T"})
	if err != nil || !found || raw != "Only plain" {
		t.Fatalf("raw=%q found=%v err=%v", raw, found, err)
	}
}

func TestLRCLib_404FallsBackToSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			w.WriteHeader(404)
		case "/api/search":
			// Two candidates; 180s track should pick the 179s one.
			w.Write([]byte(`[{"duration":300,"syncedLyrics":"[00:01.00]Wrong","plainLyrics":"W"},{"duration":179,"syncedLyrics":"[00:01.00]Right","plainLyrics":"R"}]`))
		}
	}))
	defer srv.Close()
	c := &LRCLibClient{BaseURL: srv.URL}
	raw, found, err := c.Fetch(context.Background(), Query{Artist: "A", Title: "T", DurationMs: 180_000})
	if err != nil || !found || raw != "[00:01.00]Right" {
		t.Fatalf("raw=%q found=%v err=%v", raw, found, err)
	}
}

func TestLRCLib_NoResultsIsMissNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			w.WriteHeader(404)
		case "/api/search":
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	c := &LRCLibClient{BaseURL: srv.URL}
	_, found, err := c.Fetch(context.Background(), Query{Artist: "A", Title: "T"})
	if err != nil || found {
		t.Fatalf("empty search must be a clean miss: found=%v err=%v", found, err)
	}
}

func TestLRCLib_ServerErrorIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	c := &LRCLibClient{BaseURL: srv.URL}
	_, found, err := c.Fetch(context.Background(), Query{Artist: "A", Title: "T"})
	if err == nil || found {
		t.Fatalf("5xx must surface as error: found=%v err=%v", found, err)
	}
}
