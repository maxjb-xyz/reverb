package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/crate/internal/auth"
	"github.com/maxjb-xyz/crate/internal/core"
	"github.com/maxjb-xyz/crate/internal/registry"
	"github.com/maxjb-xyz/crate/internal/store"
)

// fakeLibrary implements library.LibraryAdapter (+ browse interfaces) for tests.
type fakeLibrary struct{ lastRange string }

func (fakeLibrary) Type() string                             { return "library" }
func (fakeLibrary) Name() string                             { return "fake" }
func (fakeLibrary) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeLibrary) Init(cfg map[string]any) error            { return nil }
func (fakeLibrary) TestConnection(ctx context.Context) error { return nil }
func (fakeLibrary) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{Tracks: []core.Track{{ID: "t1", Title: "Song " + q}}}, nil
}
func (fakeLibrary) GetArtist(ctx context.Context, id string) (core.Artist, error) {
	return core.Artist{ID: id, Name: "Artist"}, nil
}
func (fakeLibrary) GetAlbum(ctx context.Context, id string) (core.Album, error) {
	return core.Album{ID: id, Name: "Album"}, nil
}
func (fakeLibrary) GetPlaylists(ctx context.Context) ([]core.Playlist, error) {
	return []core.Playlist{{ID: "p1", Name: "Mix"}}, nil
}
func (f *fakeLibrary) Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error) {
	f.lastRange = rangeHeader
	status := http.StatusOK
	cr := ""
	if rangeHeader != "" {
		status = http.StatusPartialContent
		cr = "bytes 0-3/100"
	}
	return core.StreamHandle{
		Body:          io.NopCloser(strings.NewReader("abcd")),
		ContentType:   "audio/mpeg",
		ContentLength: 4,
		AcceptRanges:  "bytes",
		ContentRange:  cr,
		StatusCode:    status,
	}, nil
}
func (fakeLibrary) CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error) {
	return core.CoverArt{Body: io.NopCloser(strings.NewReader("img")), ContentType: "image/jpeg"}, nil
}
func (fakeLibrary) StartScan(ctx context.Context) error { return nil }
func (fakeLibrary) ScanStatus(ctx context.Context) (core.ScanStatus, error) {
	return core.ScanStatus{}, nil
}
func (fakeLibrary) GetArtistsBrowse(ctx context.Context) ([]core.Artist, error) {
	return []core.Artist{{ID: "ar1", Name: "Artist"}}, nil
}
func (fakeLibrary) GetAlbumsBrowse(ctx context.Context, listType string, size int) ([]core.Album, error) {
	return []core.Album{{ID: "al1", Name: "Album"}}, nil
}

func libTestServer(t *testing.T, lib *fakeLibrary) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, err := authSvc.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{
		Auth:       authSvc,
		Library:    lib,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func doAuthed(t *testing.T, srv *Server, method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLibrarySearchHandler(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/library/search?q=hello", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var res core.SearchResults
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 1 || res.Tracks[0].Title != "Song hello" {
		t.Fatalf("results: %+v", res)
	}
}

func TestLibrarySearchRequiresAuth(t *testing.T) {
	srv, _ := libTestServer(t, &fakeLibrary{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/library/search?q=x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLibraryArtistAlbumPlaylistsHandlers(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	for _, tc := range []struct {
		path string
		want string
	}{
		{"/api/v1/library/artist/ar1", "Artist"},
		{"/api/v1/library/album/al1", "Album"},
		{"/api/v1/library/artists", "Artist"},
		{"/api/v1/library/albums?type=newest", "Album"},
		{"/api/v1/library/playlists", "Mix"},
	} {
		rec := doAuthed(t, srv, http.MethodGet, tc.path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s body missing %q: %s", tc.path, tc.want, rec.Body.String())
		}
	}
}

func TestLibraryNilAdapterReturns503(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/n.db")
	t.Cleanup(func() { st.Close() })
	_ = st.Migrate()
	authSvc := auth.NewService(st.Q(), time.Now)
	_ = authSvc.SetAdminPassword(context.Background(), "pw")
	tok, _ := authSvc.CreateSession(context.Background())
	srv := NewServer(Deps{Auth: authSvc, Library: nil,
		Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")})
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/library/search?q=x", &http.Cookie{Name: sessionCookie, Value: tok})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
