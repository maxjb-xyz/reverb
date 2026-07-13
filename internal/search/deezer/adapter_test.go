package deezer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/search/deezer"
)

const trackJSON = `{"id": 3135556, "title": "Harder, Better, Faster, Stronger",
  "duration": 224,
  "artist": {"id": 27, "name": "Daft Punk"},
  "album": {"id": 302127, "title": "Discovery",
    "cover_medium": "https://cdn.example/m.jpg", "cover_big": "https://cdn.example/b.jpg"}}`

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search/track", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": [` + trackJSON + `]}`))
	})
	mux.HandleFunc("/search/album", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": [{"id": 302127, "title": "Discovery",
			"cover_medium": "https://cdn.example/m.jpg",
			"artist": {"id": 27, "name": "Daft Punk"}}]}`))
	})
	mux.HandleFunc("/search/artist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": [{"id": 27, "name": "Daft Punk",
			"picture_medium": "https://cdn.example/a.jpg"}]}`))
	})
	mux.HandleFunc("/track/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(trackJSON))
	})
	mux.HandleFunc("/artist/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/albums") {
			w.Write([]byte(`{"data": [{"id": 302127, "title": "Discovery", "cover_medium": "https://cdn.example/m.jpg", "artist": {"id": 27, "name": "Daft Punk"}}]}`))
			return
		}
		w.Write([]byte(`{"id": 27, "name": "Daft Punk", "picture_medium": "https://cdn.example/a.jpg", "picture_big": "https://cdn.example/a-big.jpg"}`))
	})
	mux.HandleFunc("/album/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			// Deezer's in-band error shape: HTTP 200 + error object
			w.Write([]byte(`{"error": {"type": "DataException", "message": "no data", "code": 800}}`))
			return
		}
		w.Write([]byte(`{"id": 302127, "title": "Discovery",
			"cover_big": "https://cdn.example/b.jpg", "release_date": "2001-03-07",
			"artist": {"id": 27, "name": "Daft Punk"},
			"tracks": {"data": [{"id": 3135556, "title": "Harder, Better, Faster, Stronger",
				"duration": 224, "artist": {"id": 27, "name": "Daft Punk"},
				"album": {"id": 0, "title": "", "cover_medium": "", "cover_big": ""}}]}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newAdapter(t *testing.T) *deezer.Adapter {
	t.Helper()
	srv := fixtureServer(t)
	a := deezer.New().WithBaseURL(srv.URL).WithHTTPClient(srv.Client())
	if err := a.Init(map[string]any{}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return a
}

func TestConformance(t *testing.T) {
	search.RunConformance(t, newAdapter(t))
}

func TestSearchTrackMapping(t *testing.T) {
	a := newAdapter(t)
	res, err := a.Search(context.Background(), "daft punk", core.EntityTrack)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	r := res[0]
	if r.Source != "deezer" || r.ExternalID != "3135556" {
		t.Errorf("Source/ExternalID = %q/%q", r.Source, r.ExternalID)
	}
	if r.DurationMs != 224000 {
		t.Errorf("DurationMs = %d, want 224000 (seconds converted to ms)", r.DurationMs)
	}
	if r.CoverURL != "https://cdn.example/b.jpg" {
		t.Errorf("CoverURL = %q, want cover_big", r.CoverURL)
	}
	if r.ArtistExternalID != "27" || r.AlbumExternalID != "302127" {
		t.Errorf("ref ids = %q/%q", r.ArtistExternalID, r.AlbumExternalID)
	}
	if r.ISRC != "" {
		t.Errorf("ISRC should be empty for deezer search results, got %q", r.ISRC)
	}
}

func TestGetAlbumBackfillsTrackAlbumFields(t *testing.T) {
	a := newAdapter(t)
	al, err := a.GetAlbum(context.Background(), "302127")
	if err != nil {
		t.Fatalf("GetAlbum: %v", err)
	}
	if al.Year != 2001 {
		t.Errorf("Year = %d, want 2001", al.Year)
	}
	if len(al.Tracks) != 1 {
		t.Fatalf("want 1 track, got %d", len(al.Tracks))
	}
	tr := al.Tracks[0]
	if tr.Album != "Discovery" || tr.AlbumExternalID != "302127" {
		t.Errorf("track album backfill: Album=%q AlbumExternalID=%q", tr.Album, tr.AlbumExternalID)
	}
	if tr.CoverURL != "https://cdn.example/b.jpg" {
		t.Errorf("track cover backfill: %q", tr.CoverURL)
	}
}

func TestDirectTrackAndArtistLookups(t *testing.T) {
	a := newAdapter(t)
	track, err := a.GetTrack(context.Background(), "3135556")
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if track.ArtistExternalID != "27" || track.AlbumExternalID != "302127" {
		t.Errorf("track reference ids = %q/%q", track.ArtistExternalID, track.AlbumExternalID)
	}
	artist, err := a.GetArtist(context.Background(), "27")
	if err != nil {
		t.Fatalf("GetArtist: %v", err)
	}
	if artist.Source != "deezer" || artist.ExternalID != "27" || artist.CoverURL != "https://cdn.example/a-big.jpg" {
		t.Errorf("artist = %+v", artist)
	}
}

func TestGetArtistDiscography(t *testing.T) {
	albums, err := newAdapter(t).GetArtistDiscography(context.Background(), "27")
	if err != nil {
		t.Fatalf("GetArtistDiscography: %v", err)
	}
	if len(albums) != 1 || albums[0].Source != "deezer" || albums[0].ExternalID != "302127" {
		t.Fatalf("albums = %+v", albums)
	}
}

func TestInBandErrorSurfacesAsError(t *testing.T) {
	a := newAdapter(t)
	if _, err := a.GetAlbum(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for in-band Deezer error payload, got nil")
	}
}

func TestTestConnection(t *testing.T) {
	a := newAdapter(t)
	if err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	uninit := deezer.New()
	if err := uninit.TestConnection(context.Background()); err == nil {
		t.Fatal("expected error on uninitialized adapter")
	}
}
