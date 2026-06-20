package subsonic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestTokenAuthParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice", "secret", srv.Client())
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	if gotQuery.Get("u") != "alice" {
		t.Errorf("u = %q, want alice", gotQuery.Get("u"))
	}
	if gotQuery.Get("v") != "1.16.1" || gotQuery.Get("c") != "crate" || gotQuery.Get("f") != "json" {
		t.Errorf("missing fixed params: %v", gotQuery)
	}
	salt := gotQuery.Get("s")
	if len(salt) < 8 {
		t.Fatalf("salt too short: %q", salt)
	}
	// token must be md5(password + salt)
	sum := md5.Sum([]byte("secret" + salt))
	wantTok := hex.EncodeToString(sum[:])
	if gotQuery.Get("t") != wantTok {
		t.Errorf("t = %q, want %q", gotQuery.Get("t"), wantTok)
	}
	if gotQuery.Get("p") != "" {
		t.Error("must not send plaintext password param p")
	}
}

func TestGetJSONMapsFailedStatusToError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice", "bad", srv.Client())
	// GetJSON takes a *subsonicResponse (or nil to skip payload decode).
	err := c.GetJSON(context.Background(), "ping", nil, nil)
	if err == nil {
		t.Fatal("expected error for failed status")
	}
	if got := err.Error(); got == "" ||
		!contains(got, "40") || !contains(got, "Wrong username or password") {
		t.Fatalf("error missing code/message: %q", got)
	}
}

func TestRawGetForwardsRangeAndReturnsBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range not forwarded: %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abcd"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice", "secret", srv.Client())
	resp, err := c.RawGet(context.Background(), "stream", nil, "bytes=0-3")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
