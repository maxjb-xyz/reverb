package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamProxyForwardsRangeAnd206(t *testing.T) {
	lib := &fakeLibrary{}
	srv, cookie := libTestServer(t, lib)

	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/stream/t1", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-range status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("accept-ranges = %q", rec.Header().Get("Accept-Ranges"))
	}

	// With Range → 206 + Content-Range passthrough; range forwarded to adapter.
	r2rec := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/v1/stream/t1", nil)
	r2.AddCookie(cookie)
	r2.Header.Set("Range", "bytes=0-3")
	srv.Handler().ServeHTTP(r2rec, r2)
	if r2rec.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", r2rec.Code)
	}
	if r2rec.Header().Get("Content-Range") == "" {
		t.Fatal("missing Content-Range passthrough")
	}
	if lib.lastRange != "bytes=0-3" {
		t.Fatalf("range not forwarded to adapter: %q", lib.lastRange)
	}
}

func TestCoverProxy(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/cover/al-1?size=300", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}
