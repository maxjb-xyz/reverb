package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
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

// --- sentinel routing tests ---

// stubLibrary is a minimal library.LibraryAdapter whose CoverArt and Stream
// return a caller-configured error (or a trivial success value when nil).
type stubLibrary struct {
	coverErr  error
	streamErr error
}

// Satisfy registry.Plugin.
func (s *stubLibrary) Type() string                             { return "library" }
func (s *stubLibrary) Name() string                             { return "stub" }
func (s *stubLibrary) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (s *stubLibrary) Init(_ map[string]any) error              { return nil }
func (s *stubLibrary) TestConnection(_ context.Context) error   { return nil }

// Satisfy library.LibraryAdapter.
func (s *stubLibrary) Search(_ context.Context, _ string, _ []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{}, nil
}
func (s *stubLibrary) GetArtist(_ context.Context, _ string) (core.Artist, error) {
	return core.Artist{}, nil
}
func (s *stubLibrary) GetAlbum(_ context.Context, _ string) (core.Album, error) {
	return core.Album{}, nil
}
func (s *stubLibrary) GetPlaylists(_ context.Context) ([]core.Playlist, error) {
	return nil, nil
}
func (s *stubLibrary) GetPlaylist(_ context.Context, _ string) (core.Playlist, error) {
	return core.Playlist{}, nil
}
func (s *stubLibrary) CreatePlaylist(_ context.Context, name string) (core.Playlist, error) {
	return core.Playlist{Name: name}, nil
}
func (s *stubLibrary) AddTracksToPlaylist(_ context.Context, _ string, _ []string) error {
	return nil
}
func (s *stubLibrary) StartScan(_ context.Context) error { return nil }
func (s *stubLibrary) ScanStatus(_ context.Context) (core.ScanStatus, error) {
	return core.ScanStatus{}, nil
}
func (s *stubLibrary) CoverArt(_ context.Context, _ string, _ int) (core.CoverArt, error) {
	if s.coverErr != nil {
		return core.CoverArt{}, s.coverErr
	}
	return core.CoverArt{Body: io.NopCloser(strings.NewReader("img")), ContentType: "image/jpeg"}, nil
}
func (s *stubLibrary) Stream(_ context.Context, _ string, _ core.StreamOpts, _ string) (core.StreamHandle, error) {
	if s.streamErr != nil {
		return core.StreamHandle{}, s.streamErr
	}
	return core.StreamHandle{
		Body:        io.NopCloser(strings.NewReader("abcd")),
		ContentType: "audio/mpeg",
		StatusCode:  http.StatusOK,
	}, nil
}

// compile-time check
var _ library.LibraryAdapter = (*stubLibrary)(nil)

// stubLibTestServer builds a Server wired to an arbitrary library.LibraryAdapter.
func stubLibTestServer(t *testing.T, lib library.LibraryAdapter) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/stub.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	uid, err := authSvc.SetupOwner(context.Background(), "owner", "pw")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := authSvc.CreateSession(context.Background(), uid)
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

func TestHandlerStream_ErrLibraryItemNotFound_Returns404(t *testing.T) {
	// When Stream returns an error wrapping core.ErrLibraryItemNotFound, the
	// handler must respond 404, not 502.
	lib := &stubLibrary{streamErr: fmt.Errorf("stale track: %w", core.ErrLibraryItemNotFound)}
	srv, cookie := stubLibTestServer(t, lib)
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/stream/dead-id", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerStream_TransportError_Returns502(t *testing.T) {
	// When Stream returns a plain (non-sentinel) error, the handler must keep
	// responding 502 Bad Gateway.
	lib := &stubLibrary{streamErr: errors.New("connection refused")}
	srv, cookie := stubLibTestServer(t, lib)
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/stream/any", cookie)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerCover_ErrLibraryItemNotFound_Returns404(t *testing.T) {
	// When CoverArt returns an error wrapping core.ErrLibraryItemNotFound, the
	// handler must respond 404.
	lib := &stubLibrary{coverErr: fmt.Errorf("no artwork: %w", core.ErrLibraryItemNotFound)}
	srv, cookie := stubLibTestServer(t, lib)
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/cover/dead-id", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlerCover_TransportError_Returns502(t *testing.T) {
	// A plain transport error from CoverArt must stay 502.
	lib := &stubLibrary{coverErr: errors.New("connection refused")}
	srv, cookie := stubLibTestServer(t, lib)
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/cover/any", cookie)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}
