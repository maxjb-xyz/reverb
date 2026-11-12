package subsonic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/library"
)

func ok(w http.ResponseWriter, body map[string]any) {
	body["status"] = "ok"
	body["version"] = "1.16.1"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
}

func song(i int) map[string]any {
	return map[string]any{
		"id": fmt.Sprintf("s%d", i), "title": fmt.Sprintf("Song %d", i), "album": "Alb", "albumId": "al1",
		"artist": "Art", "artistId": "ar1", "duration": 200, "year": 1999, "genre": "Rock",
		"genres": []map[string]string{{"name": "Rock"}, {"name": "Indie"}}, "created": "2024-05-01T10:00:00Z",
	}
}

func TestWalkTracks_PagesSearch3(t *testing.T) {
	const total = 1203
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) != "search3" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		q := r.URL.Query()
		queries = append(queries, q.Get("query"))
		off, _ := strconv.Atoi(q.Get("songOffset"))
		n, _ := strconv.Atoi(q.Get("songCount"))
		var songs []map[string]any
		for i := off; i < off+n && i < total; i++ {
			songs = append(songs, song(i))
		}
		ok(w, map[string]any{"searchResult3": map[string]any{"song": songs}})
	}))
	defer srv.Close()
	a := walkAdapter(t, srv)

	var got []library.IndexedTrack
	var walker library.TrackWalker = a
	if err := walker.WalkTracks(context.Background(), func(b []library.IndexedTrack) error {
		got = append(got, b...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != total {
		t.Fatalf("walked %d tracks, want %d", len(got), total)
	}
	if queries[0] != `""` {
		t.Fatalf("search3 query = %q, want the empty-quoted list-all form", queries[0])
	}
	f := got[0]
	if f.Year != 1999 || len(f.Genres) != 2 || f.Genres[1] != "Indie" || f.AddedAt == 0 || f.DurationMs != 200_000 {
		t.Fatalf("facets not mapped: %+v", f)
	}
}

func TestWalkTracks_FallsBackToAlbumWalk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch filepath.Base(r.URL.Path) {
		case "search3":
			ok(w, map[string]any{"searchResult3": map[string]any{}}) // server doesn't support list-all
		case "getAlbumList2":
			if q.Get("offset") != "0" {
				ok(w, map[string]any{"albumList2": map[string]any{}})
				return
			}
			ok(w, map[string]any{"albumList2": map[string]any{"album": []map[string]any{{"id": "al1", "name": "A"}, {"id": "al2", "name": "B"}}}})
		case "getAlbum":
			s := song(1)
			s["id"] = "track-of-" + q.Get("id")
			s["genres"] = nil
			s["genre"] = "Jazz; Soul"
			ok(w, map[string]any{"album": map[string]any{"id": q.Get("id"), "name": "x", "song": []map[string]any{s}}})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	a := walkAdapter(t, srv)
	var got []library.IndexedTrack
	if err := a.WalkTracks(context.Background(), func(b []library.IndexedTrack) error { got = append(got, b...); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ID != "track-of-al2" {
		t.Fatalf("album walk = %+v", got)
	}
	if len(got[0].Genres) != 2 || got[0].Genres[0] != "Jazz" || got[0].Genres[1] != "Soul" {
		t.Fatalf("single genre tag must split on ';': %+v", got[0].Genres)
	}
}

func TestWalkTracks_StopsOnCallbackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"searchResult3": map[string]any{"song": []map[string]any{song(1)}}})
	}))
	defer srv.Close()
	a := walkAdapter(t, srv)
	stop := fmt.Errorf("stop")
	calls := 0
	err := a.WalkTracks(context.Background(), func([]library.IndexedTrack) error { calls++; return stop })
	if err != stop || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func walkAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	a := New().WithHTTPClient(srv.Client())
	if err := a.Init(map[string]any{"url": srv.URL, "username": "u", "password": "p"}); err != nil {
		t.Fatal(err)
	}
	return a
}
