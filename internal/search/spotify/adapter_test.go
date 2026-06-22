package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	if r.ArtistExternalID != "sp_ar1" {
		t.Fatalf("ArtistExternalID not mapped: %q (want %q)", r.ArtistExternalID, "sp_ar1")
	}
	if r.AlbumExternalID != "sp_al1" {
		t.Fatalf("AlbumExternalID not mapped: %q (want %q)", r.AlbumExternalID, "sp_al1")
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

func TestParsePlaylistID(t *testing.T) {
	cases := map[string]string{
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M":        "37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc": "37i9dQZF1DXcBWIGoYBM5M",
		"spotify:playlist:37i9dQZF1DXcBWIGoYBM5M":                        "37i9dQZF1DXcBWIGoYBM5M",
	}
	for in, want := range cases {
		got, ok := ParsePlaylistID(in)
		if !ok || got != want {
			t.Fatalf("ParsePlaylistID(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}
	if _, ok := ParsePlaylistID("https://open.spotify.com/album/123"); ok {
		t.Fatal("album URL must not parse as a playlist")
	}
}

func TestGetPlaylistMapsAndPaginates(t *testing.T) {
	page1 := `{"name":"Chill","images":[{"url":"http://img/p"}],"tracks":{
	  "items":[{"track":{"id":"t1","name":"One","artists":[{"name":"A"}],"duration_ms":1000,"external_ids":{"isrc":"X1"},"album":{"name":"AL","images":[{"url":"http://img/a"}]}}}],
	  "next":"NEXTURL"}}`
	page2 := `{"items":[{"track":{"id":"t2","name":"Two","artists":[{"name":"B"}],"duration_ms":2000,"album":{"name":"AL2"}}}],"next":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/playlists/") && strings.HasSuffix(r.URL.Path, "/tracks"):
			_, _ = w.Write([]byte(page2)) // the "next" page
		case strings.Contains(r.URL.Path, "/playlists/"):
			_, _ = w.Write([]byte(page1)) // playlist object (meta + first tracks page)
		default:
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
		}
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	pl, err := a.GetPlaylist(context.Background(), "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if pl.Name != "Chill" || pl.CoverURL != "http://img/p" {
		t.Fatalf("bad meta: %+v", pl)
	}
	if len(pl.Tracks) != 2 || pl.Tracks[0].ExternalID != "t1" || pl.Tracks[1].ExternalID != "t2" {
		t.Fatalf("bad tracks: %+v", pl.Tracks)
	}
	if pl.Tracks[0].ISRC != "X1" || pl.Tracks[0].Type != core.EntityTrack {
		t.Fatalf("bad track mapping: %+v", pl.Tracks[0])
	}
}

func TestGetPlaylistPaginationOffsetUsesItemsSeen(t *testing.T) {
	// Page 1: 2 items, BOTH local tracks (empty id) + next != "".
	// Page 2: 1 real track, next == null.
	// The offset for page 2 must be 2 (items SEEN), not 0 (accepted tracks).
	page1 := `{"name":"Local","images":[],"tracks":{
	  "items":[
	    {"track":{"id":"","name":"Local1","artists":[],"duration_ms":0,"album":{"name":"","images":[]}}},
	    {"track":{"id":"","name":"Local2","artists":[],"duration_ms":0,"album":{"name":"","images":[]}}}
	  ],
	  "next":"NEXTURL"}}`
	page2 := `{"items":[{"track":{"id":"real1","name":"Real","artists":[{"name":"C"}],"duration_ms":3000,"album":{"name":"AL3"}}}],"next":null}`

	var capturedOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/playlists/") && strings.HasSuffix(r.URL.Path, "/tracks"):
			capturedOffset = r.URL.Query().Get("offset")
			_, _ = w.Write([]byte(page2))
		case strings.Contains(r.URL.Path, "/playlists/"):
			_, _ = w.Write([]byte(page1))
		default:
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
		}
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	pl, err := a.GetPlaylist(context.Background(), "pl2")
	if err != nil {
		t.Fatal(err)
	}
	// (a) offset must be 2 (items seen), not 0 (accepted tracks)
	if capturedOffset != "2" {
		t.Fatalf("want offset=2, got %q (offset must count all items seen, not just accepted tracks)", capturedOffset)
	}
	// (b) exactly 1 real track; loop terminated (no hang)
	if len(pl.Tracks) != 1 || pl.Tracks[0].ExternalID != "real1" {
		t.Fatalf("want 1 track with id=real1, got %+v", pl.Tracks)
	}
}

func TestGetArtistMapsProfile(t *testing.T) {
	fixture := `{"id":"sp_ar1","name":"Frédéric Chopin","images":[{"url":"https://img/chopin_large.jpg","width":640,"height":640},{"url":"https://img/chopin_small.jpg","width":160,"height":160}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/artists/") && !strings.HasSuffix(r.URL.Path, "/albums") {
			_, _ = w.Write([]byte(fixture))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	prof, err := a.GetArtist(context.Background(), "sp_ar1")
	if err != nil {
		t.Fatal(err)
	}
	if prof.Name != "Frédéric Chopin" {
		t.Errorf("Name: want %q, got %q", "Frédéric Chopin", prof.Name)
	}
	if prof.CoverURL != "https://img/chopin_large.jpg" {
		t.Errorf("CoverURL: want %q, got %q", "https://img/chopin_large.jpg", prof.CoverURL)
	}
	if prof.Source != "spotify" {
		t.Errorf("Source: want %q, got %q", "spotify", prof.Source)
	}
	if prof.ExternalID != "sp_ar1" {
		t.Errorf("ExternalID: want %q, got %q", "sp_ar1", prof.ExternalID)
	}
}

func TestGetArtistDiscographyCappedAtMaxPages(t *testing.T) {
	// Build a response page with 50 items and next != "". The handler counts how
	// many times the /albums endpoint is called; the loop must stop at maxDiscographyPages.
	const pageSize = 50
	pageBody := func(next string) string {
		items := make([]string, pageSize)
		for i := range items {
			items[i] = `{"id":"al` + strconv.Itoa(i) + `","name":"Album","album_type":"album","total_tracks":1,"release_date":"2020","images":[],"artists":[{"id":"a","name":"Prolific"}]}`
		}
		nextVal := "null"
		if next != "" {
			nextVal = `"` + next + `"`
		}
		return `{"items":[` + strings.Join(items, ",") + `],"next":` + nextVal + `}`
	}

	var albumsCallCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/albums"):
			albumsCallCount++
			// Always return a full page with a next URL so it would loop forever without cap.
			_, _ = w.Write([]byte(pageBody("http://next")))
		default:
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
		}
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	albums, err := a.GetArtistDiscography(context.Background(), "prolific_artist")
	if err != nil {
		t.Fatal(err)
	}
	if albumsCallCount != maxDiscographyPages {
		t.Errorf("expected exactly %d page requests, got %d", maxDiscographyPages, albumsCallCount)
	}
	wantAlbums := maxDiscographyPages * pageSize
	if len(albums) != wantAlbums {
		t.Errorf("expected %d albums (%d pages × %d per page), got %d", wantAlbums, maxDiscographyPages, pageSize, len(albums))
	}
}

func TestGetArtistDiscographyMapsAndFilters(t *testing.T) {
	page := `{"items":[
	  {"id":"al1","name":"OK Computer","album_type":"album","total_tracks":12,"release_date":"1997-05-21","images":[{"url":"http://img/1"}],"artists":[{"id":"art1","name":"Radiohead"}]},
	  {"id":"s1","name":"Creep","album_type":"single","total_tracks":1,"release_date":"1992-09-21","images":[{"url":"http://img/2"}],"artists":[{"id":"art1","name":"Radiohead"}]}
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
	if albums[0].Artist != "Radiohead" {
		t.Fatalf("album Artist not populated: %q", albums[0].Artist)
	}
	if albums[1].Kind != "single" {
		t.Fatalf("want single, got %q", albums[1].Kind)
	}
}
