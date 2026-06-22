package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// fixtureServer serves token + search/album fixtures based on the path & type.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	serve := func(w http.ResponseWriter, file string) {
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatalf("read fixture %s: %v", file, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) { serve(w, "token.json") })
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("type") {
		case "album":
			serve(w, "search_albums.json")
		case "artist":
			serve(w, "search_artists.json")
		default:
			serve(w, "search_tracks.json")
		}
	})
	mux.HandleFunc("/v1/albums/", func(w http.ResponseWriter, r *http.Request) { serve(w, "album.json") })
	return httptest.NewServer(mux)
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	srv := fixtureServer(t)
	t.Cleanup(srv.Close)
	a := New().WithHTTPClient(srv.Client()).WithBaseURLs(srv.URL, srv.URL+"/v1")
	if err := a.Init(map[string]any{"client_id": "cid", "client_secret": "csecret"}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapterIdentityAndSchema(t *testing.T) {
	a := New()
	if a.Type() != "search" || a.Name() != "spotify" {
		t.Fatalf("identity: %q/%q", a.Type(), a.Name())
	}
	secret := map[string]bool{}
	for _, f := range a.ConfigSchema().Fields {
		secret[f.Key] = f.Secret
	}
	if _, ok := secret["client_id"]; !ok {
		t.Error("schema missing client_id")
	}
	if s, ok := secret["client_secret"]; !ok || !s {
		t.Error("client_secret missing or not marked secret")
	}
}

func TestSearchTracksMapsISRCAndCover(t *testing.T) {
	a := newTestAdapter(t)
	res, err := a.Search(context.Background(), "x", core.EntityTrack)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 tracks, got %d", len(res))
	}
	r := res[0]
	if r.Source != "spotify" || r.ExternalID != "sp_t1" || r.Title != "Opening" {
		t.Fatalf("track0: %+v", r)
	}
	if r.Artist != "The Artists" || r.Album != "First Album" || r.DurationMs != 210000 {
		t.Fatalf("track0 fields: %+v", r)
	}
	if r.ISRC != "USX1234567" {
		t.Fatalf("ISRC not mapped: %q", r.ISRC)
	}
	if r.CoverURL != "https://img/al1.jpg" {
		t.Fatalf("cover not mapped: %q", r.CoverURL)
	}
	if r.Type != core.EntityTrack {
		t.Fatalf("type: %q", r.Type)
	}
	if res[1].ISRC != "" {
		t.Fatalf("track1 should have empty ISRC, got %q", res[1].ISRC)
	}
}

func TestSearchAlbumsAndArtists(t *testing.T) {
	a := newTestAdapter(t)
	als, err := a.Search(context.Background(), "x", core.EntityAlbum)
	if err != nil {
		t.Fatal(err)
	}
	if len(als) != 1 || als[0].Title != "First Album" || als[0].Type != core.EntityAlbum {
		t.Fatalf("albums: %+v", als)
	}
	ars, err := a.Search(context.Background(), "x", core.EntityArtist)
	if err != nil {
		t.Fatal(err)
	}
	if len(ars) != 1 || ars[0].Title != "The Artists" || ars[0].Type != core.EntityArtist {
		t.Fatalf("artists: %+v", ars)
	}
}

func TestGetAlbumIncludesTracks(t *testing.T) {
	a := newTestAdapter(t)
	al, err := a.GetAlbum(context.Background(), "sp_al1")
	if err != nil {
		t.Fatal(err)
	}
	if al.Name != "First Album" || al.Year != 2020 || len(al.Tracks) != 2 {
		t.Fatalf("album: %+v", al)
	}
	if al.Tracks[0].ISRC != "USX1234567" {
		t.Fatalf("album track ISRC: %q", al.Tracks[0].ISRC)
	}
}

func TestSpotifyConformance(t *testing.T) {
	a := newTestAdapter(t)
	search.RunConformance(t, a)
}

func TestGetArtistDiscographyMapsAndFilters(t *testing.T) {
	page := `{"items":[
	  {"id":"al1","name":"OK Computer","album_type":"album","total_tracks":12,"release_date":"1997-05-21","images":[{"url":"http://img/1"}]},
	  {"id":"s1","name":"Creep","album_type":"single","total_tracks":1,"release_date":"1992-09-21","images":[{"url":"http://img/2"}]}
	],"next":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/artists/") && strings.HasSuffix(r.URL.Path, "/albums") {
			_, _ = w.Write([]byte(page))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	albums, err := a.GetArtistDiscography(context.Background(), "art1")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("want 2 albums, got %d", len(albums))
	}
	if albums[0].Name != "OK Computer" || albums[0].Kind != "album" || albums[0].TotalTracks != 12 || albums[0].Year != 1997 {
		t.Fatalf("bad album mapping: %+v", albums[0])
	}
	if albums[1].Kind != "single" {
		t.Fatalf("want single, got %q", albums[1].Kind)
	}
}
