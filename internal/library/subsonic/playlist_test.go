package subsonic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// newRecordingAdapter spins up an httptest.Server that records the query of the
// last request and serves the given body, returning a configured adapter.
func newRecordingAdapter(t *testing.T, status int, body string, gotQuery *url.Values) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	a := New().WithHTTPClient(srv.Client())
	if err := a.Init(map[string]any{"url": srv.URL, "username": "alice", "password": "secret"}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCreatePlaylistIssuesRequestAndParses(t *testing.T) {
	var got url.Values
	body := `{"subsonic-response":{"status":"ok","version":"1.16.1","playlist":{"id":"pl-99","name":"Road Trip","songCount":0}}}`
	a := newRecordingAdapter(t, http.StatusOK, body, &got)

	pl, err := a.CreatePlaylist(context.Background(), "Road Trip")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("name") != "Road Trip" {
		t.Errorf("name param = %q, want %q", got.Get("name"), "Road Trip")
	}
	if pl.ID != "pl-99" || pl.Name != "Road Trip" || pl.SongCount != 0 {
		t.Fatalf("playlist mapping: %+v", pl)
	}
}

// Older servers may return "ok" with no playlist body — we synthesize from name.
func TestCreatePlaylistWithoutBodyFallsBackToName(t *testing.T) {
	var got url.Values
	body := `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`
	a := newRecordingAdapter(t, http.StatusOK, body, &got)

	pl, err := a.CreatePlaylist(context.Background(), "Empty")
	if err != nil {
		t.Fatal(err)
	}
	if pl.Name != "Empty" || pl.SongCount != 0 {
		t.Fatalf("fallback playlist: %+v", pl)
	}
}

func TestAddTracksToPlaylistIssuesUpdatePlaylist(t *testing.T) {
	var got url.Values
	body := `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`
	a := newRecordingAdapter(t, http.StatusOK, body, &got)

	if err := a.AddTracksToPlaylist(context.Background(), "pl-1", []string{"t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	if got.Get("playlistId") != "pl-1" {
		t.Errorf("playlistId = %q, want pl-1", got.Get("playlistId"))
	}
	adds := got["songIdToAdd"]
	if len(adds) != 2 || adds[0] != "t1" || adds[1] != "t2" {
		t.Fatalf("songIdToAdd params = %v, want [t1 t2]", adds)
	}
}

func TestAddTracksToPlaylistSurfacesUpstreamError(t *testing.T) {
	var got url.Values
	body := `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"Playlist not found"}}}`
	a := newRecordingAdapter(t, http.StatusOK, body, &got)

	err := a.AddTracksToPlaylist(context.Background(), "missing", []string{"t1"})
	if err == nil {
		t.Fatal("expected error from failed subsonic status")
	}
	if !contains(err.Error(), "Playlist not found") {
		t.Fatalf("error missing upstream message: %q", err.Error())
	}
}

func TestCreatePlaylistSurfacesUpstreamError(t *testing.T) {
	var got url.Values
	body := `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":50,"message":"Not authorized"}}}`
	a := newRecordingAdapter(t, http.StatusOK, body, &got)

	_, err := a.CreatePlaylist(context.Background(), "X")
	if err == nil {
		t.Fatal("expected error from failed subsonic status")
	}
}
