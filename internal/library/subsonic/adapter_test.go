package subsonic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/library"
)

// fixtureServer serves a recorded JSON file based on the requested endpoint
// (the last path segment of /rest/<endpoint>). Unknown endpoints → ping.json.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"ping":          "ping.json",
		"search3":       "search3.json",
		"getArtists":    "getArtists.json",
		"getArtist":     "getArtist.json",
		"getAlbum":      "getAlbum.json",
		"getAlbumList2": "getAlbumList2.json",
		"getPlaylists":  "getPlaylists.json",
		"getScanStatus": "getScanStatus.json",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := filepath.Base(r.URL.Path)
		switch endpoint {
		case "stream":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Accept-Ranges", "bytes")
			if r.Header.Get("Range") != "" {
				w.Header().Set("Content-Range", "bytes 0-3/100")
				w.WriteHeader(http.StatusPartialContent)
			}
			_, _ = w.Write([]byte("abcd"))
			return
		case "getCoverArt":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("img-bytes"))
			return
		case "startScan":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","scanStatus":{"scanning":true,"count":0}}}`))
			return
		}
		file, ok := routes[endpoint]
		if !ok {
			file = "ping.json"
		}
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatalf("read fixture %s: %v", file, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	srv := fixtureServer(t)
	t.Cleanup(srv.Close)
	a := New().WithHTTPClient(srv.Client())
	if err := a.Init(map[string]any{
		"url":      srv.URL,
		"username": "alice",
		"password": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapterIdentityAndSchema(t *testing.T) {
	a := New()
	if a.Type() != "library" || a.Name() != "subsonic" {
		t.Fatalf("identity: %q/%q", a.Type(), a.Name())
	}
	keys := map[string]bool{}
	for _, f := range a.ConfigSchema().Fields {
		keys[f.Key] = f.Secret
	}
	if _, ok := keys["url"]; !ok {
		t.Error("schema missing url")
	}
	if _, ok := keys["username"]; !ok {
		t.Error("schema missing username")
	}
	if secret, ok := keys["password"]; !ok || !secret {
		t.Error("schema missing password or password not marked secret")
	}
}

func TestInitRequiresPassword(t *testing.T) {
	a := New()
	err := a.Init(map[string]any{
		"url":      "http://x",
		"username": "u",
	})
	if err == nil {
		t.Fatal("Init with empty password: want non-nil error, got nil")
	}
}

func TestSearchMapsToCore(t *testing.T) {
	a := newTestAdapter(t)
	res, err := a.Search(context.Background(), "x", []core.EntityType{core.EntityTrack, core.EntityAlbum, core.EntityArtist})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 1 || res.Tracks[0].ID != "t1" || res.Tracks[0].Title != "Opening" {
		t.Fatalf("tracks: %+v", res.Tracks)
	}
	if res.Tracks[0].DurationMs != 210000 {
		t.Fatalf("duration not seconds→ms: %d", res.Tracks[0].DurationMs)
	}
	if len(res.Albums) != 1 || res.Albums[0].Name != "First Album" {
		t.Fatalf("albums: %+v", res.Albums)
	}
	if len(res.Artists) != 1 || res.Artists[0].Name != "The Artists" {
		t.Fatalf("artists: %+v", res.Artists)
	}
}

// TestMapTrackISRC verifies that the OpenSubsonic `isrc` field is forwarded to
// core.Track.ISRC (activates the confidence-1.0 ISRC match rung end-to-end) and
// that a child without `isrc` maps to an empty ISRC (graceful classic Subsonic).
func TestMapTrackISRC(t *testing.T) {
	// With ISRC (OpenSubsonic server).
	withISRC := mapTrack(childDTO{ID: "t-isrc", Title: "Song", Isrc: "USABC1234567"})
	if withISRC.ISRC != "USABC1234567" {
		t.Errorf("ISRC: got %q, want %q", withISRC.ISRC, "USABC1234567")
	}
	// Without ISRC (classic Subsonic server omits the field).
	withoutISRC := mapTrack(childDTO{ID: "t-noisrc", Title: "Song"})
	if withoutISRC.ISRC != "" {
		t.Errorf("expected empty ISRC, got %q", withoutISRC.ISRC)
	}
	// ISRC also flows through via Search (fixture search3.json carries isrc).
	a := newTestAdapter(t)
	res, err := a.Search(context.Background(), "x", []core.EntityType{core.EntityTrack})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) == 0 {
		t.Fatal("no tracks returned")
	}
	if res.Tracks[0].ISRC != "USABC1234567" {
		t.Errorf("Search ISRC: got %q, want %q", res.Tracks[0].ISRC, "USABC1234567")
	}
}

func TestGetAlbumIncludesTracks(t *testing.T) {
	a := newTestAdapter(t)
	al, err := a.GetAlbum(context.Background(), "al1")
	if err != nil {
		t.Fatal(err)
	}
	if al.Name != "First Album" || len(al.Tracks) != 2 {
		t.Fatalf("album: %+v", al)
	}
	if al.Tracks[1].Title != "Closing" {
		t.Fatalf("track order: %+v", al.Tracks)
	}
}

func TestGetArtistIncludesAlbums(t *testing.T) {
	a := newTestAdapter(t)
	ar, err := a.GetArtist(context.Background(), "ar1")
	if err != nil {
		t.Fatal(err)
	}
	if ar.Name != "The Artists" || len(ar.Albums) != 2 {
		t.Fatalf("artist: %+v", ar)
	}
}

func TestStreamForwardsRange(t *testing.T) {
	a := newTestAdapter(t)
	h, err := a.Stream(context.Background(), "t1", core.StreamOpts{}, "bytes=0-3")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Body.Close()
	if h.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", h.StatusCode)
	}
	if h.ContentRange == "" {
		t.Error("missing Content-Range passthrough")
	}
}

func TestCoverArt(t *testing.T) {
	a := newTestAdapter(t)
	c, err := a.CoverArt(context.Background(), "al-1", 300)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Body.Close()
	if c.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q", c.ContentType)
	}
}

func TestScanStatus(t *testing.T) {
	a := newTestAdapter(t)
	st, err := a.ScanStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Scanning || st.Count != 1234 {
		t.Fatalf("scan status: %+v", st)
	}
}

func TestSubsonicConformance(t *testing.T) {
	a := newTestAdapter(t)
	library.RunConformance(t, a)
}

func TestGetArtistsBrowse(t *testing.T) {
	a := newTestAdapter(t)
	arts, err := a.GetArtistsBrowse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("artists: %+v", arts)
	}
}

func TestGetAlbumsBrowse(t *testing.T) {
	a := newTestAdapter(t)
	albs, err := a.GetAlbumsBrowse(context.Background(), "newest", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(albs) != 2 {
		t.Fatalf("albums: %+v", albs)
	}
}
