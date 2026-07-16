package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/coverage"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// fakeCoverage is a controllable CoverageService for handler tests.
type fakeCoverage struct {
	artist     core.ArtistDetail
	artistErr  error
	profile    core.ExternalArtist
	profileErr error
	album      core.AlbumDetail
	albumErr   error
	covs       []core.AlbumCoverage
	lastSource string
	lastID     string

	// discographies backs ListCachedDiscographies for collection tests.
	discographies []collectionFakeDiscography
	// artistTotal backs CountLibraryArtists.
	artistTotal    int
	artistCountErr error
}

// collectionFakeDiscography is a lightweight stand-in for
// coverage.CachedArtistDiscography, used to build fakeCoverage.discographies
// without importing db-shaped rows into api tests.
type collectionFakeDiscography struct {
	libraryArtistID, name, coverArtID, source, externalArtistID string
	albums                                                      []core.DiscographyAlbum
}

var errFakeCount = errors.New("fake: library artist count unavailable")

func (f *fakeCoverage) ArtistDetail(_ context.Context, source, id string) (core.ArtistDetail, error) {
	f.lastSource, f.lastID = source, id
	return f.artist, f.artistErr
}

func (f *fakeCoverage) ArtistProfile(_ context.Context, source, id string) (core.ExternalArtist, error) {
	f.lastSource, f.lastID = source, id
	return f.profile, f.profileErr
}

func (f *fakeCoverage) AlbumDetail(_ context.Context, source, id string) (core.AlbumDetail, error) {
	f.lastSource, f.lastID = source, id
	return f.album, f.albumErr
}

func (f *fakeCoverage) StreamCoverage(ctx context.Context, source, id string) <-chan core.AlbumCoverage {
	f.lastSource, f.lastID = source, id
	ch := make(chan core.AlbumCoverage)
	go func() {
		defer close(ch)
		for _, c := range f.covs {
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func (f *fakeCoverage) ListCachedDiscographies(context.Context) ([]coverage.CachedArtistDiscography, error) {
	if f.discographies == nil {
		return nil, nil
	}
	out := make([]coverage.CachedArtistDiscography, 0, len(f.discographies))
	for _, d := range f.discographies {
		out = append(out, coverage.CachedArtistDiscography{
			LibraryArtistID: d.libraryArtistID, Name: d.name, CoverArtID: d.coverArtID,
			Source: d.source, ExternalArtistID: d.externalArtistID, Albums: d.albums,
		})
	}
	return out, nil
}

func (f *fakeCoverage) CountLibraryArtists(context.Context) (int, error) {
	if f.artistCountErr != nil {
		return 0, f.artistCountErr
	}
	return f.artistTotal, nil
}

// coverageTestServer builds a Server with a fake coverage service + download manager.
func coverageTestServer(t *testing.T, cov CoverageService, mgr DownloadManager) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Coverage:   cov,
		Downloads:  mgr,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestCoverageArtistDetail(t *testing.T) {
	cov := &fakeCoverage{artist: core.ArtistDetail{
		Source: "library", ID: "abc", Name: "The Band", Resolved: true,
		Albums: []core.DiscographyAlbum{{Source: "spotify", ExternalID: "sp1", Name: "Debut"}},
	}}
	srv, cookie := coverageTestServer(t, cov, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artist/library/abc", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var det core.ArtistDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &det); err != nil {
		t.Fatal(err)
	}
	if len(det.Albums) != 1 || det.Albums[0].ExternalID != "sp1" {
		t.Fatalf("albums = %+v", det.Albums)
	}
	if cov.lastSource != "library" || cov.lastID != "abc" {
		t.Fatalf("params = %q/%q", cov.lastSource, cov.lastID)
	}
	// Body must contain the "albums" key.
	if !strings.Contains(rec.Body.String(), `"albums"`) {
		t.Fatalf("body missing albums: %s", rec.Body.String())
	}
}

func TestCoverageStreamSSE(t *testing.T) {
	cov := &fakeCoverage{covs: []core.AlbumCoverage{
		{Source: "spotify", ExternalAlbumID: "a1", State: core.CoverageFull, OwnedCount: 10, TotalCount: 10, MissingTracks: []core.ExternalTrackRef{}},
		{Source: "spotify", ExternalAlbumID: "a2", State: core.CoveragePartial, OwnedCount: 4, TotalCount: 10, MissingTracks: []core.ExternalTrackRef{}},
	}}
	srv, cookie := coverageTestServer(t, cov, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artist/spotify/xyz/coverage", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	var parsed []core.AlbumCoverage
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			var c core.AlbumCoverage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &c); err != nil {
				t.Fatalf("bad event json: %v (%q)", err, line)
			}
			parsed = append(parsed, c)
		}
	}
	if len(parsed) != 2 {
		t.Fatalf("want 2 frames, got %d: %s", len(parsed), rec.Body.String())
	}
	if parsed[0].ExternalAlbumID != "a1" || parsed[1].State != core.CoveragePartial {
		t.Fatalf("frames = %+v", parsed)
	}
}

func TestCoverageBatchDownload(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := coverageTestServer(t, &fakeCoverage{}, mgr)

	body := `{"tracks":[
		{"source":"spotify","externalId":"sp1","title":"One","artist":"A"},
		{"source":"spotify","externalId":"sp2","title":"Two","artist":"A"}
	]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/batch", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var jobs []core.DownloadJob
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d: %+v", len(jobs), jobs)
	}
	if len(mgr.jobs) != 2 {
		t.Fatalf("Enqueue called %d times, want 2", len(mgr.jobs))
	}
}

func TestArtistProfileEndpoint(t *testing.T) {
	cov := &fakeCoverage{
		profile: core.ExternalArtist{
			Source:     "spotify",
			ExternalID: "xyz",
			Name:       "Radiohead",
			CoverURL:   "https://img/rh.jpg",
		},
	}
	srv, cookie := coverageTestServer(t, cov, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artist/spotify/xyz/profile", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var prof core.ExternalArtist
	if err := json.Unmarshal(rec.Body.Bytes(), &prof); err != nil {
		t.Fatal(err)
	}
	if prof.Name != "Radiohead" {
		t.Errorf("Name: want %q, got %q", "Radiohead", prof.Name)
	}
	if prof.CoverURL != "https://img/rh.jpg" {
		t.Errorf("CoverURL: want %q, got %q", "https://img/rh.jpg", prof.CoverURL)
	}
	if cov.lastSource != "spotify" || cov.lastID != "xyz" {
		t.Errorf("params = %q/%q", cov.lastSource, cov.lastID)
	}
}

func TestArtistProfileUpstreamErrorReturns502(t *testing.T) {
	cov := &fakeCoverage{profileErr: fmt.Errorf("upstream Spotify timeout")}
	srv, cookie := coverageTestServer(t, cov, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artist/spotify/xyz/profile", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestArtistProfileNilServiceReturns503(t *testing.T) {
	srv, cookie := coverageTestServer(t, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artist/spotify/xyz/profile", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestCoverageNilServiceReturns503(t *testing.T) {
	srv, cookie := coverageTestServer(t, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artist/library/abc", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestBatchDownloadEmptyReturns400(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := coverageTestServer(t, &fakeCoverage{}, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/batch", strings.NewReader(`{"tracks":[]}`))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// A body that exceeds the 500-track cap must be rejected with 400 BEFORE any
// Enqueue is attempted (Fix 3: cap the batch-download endpoint).
func TestBatchDownloadOverCapReturns400(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := coverageTestServer(t, &fakeCoverage{}, mgr)

	var sb strings.Builder
	sb.WriteString(`{"tracks":[`)
	for i := 0; i < 501; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"source":"spotify","externalId":"sp` + strconv.Itoa(i) + `","title":"t","artist":"a"}`)
	}
	sb.WriteString(`]}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/batch", strings.NewReader(sb.String()))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "too many tracks") {
		t.Fatalf("body = %q, want \"too many tracks\"", rec.Body.String())
	}
	if len(mgr.jobs) != 0 {
		t.Fatalf("Enqueue called %d times, want 0 (rejected before enqueue)", len(mgr.jobs))
	}
}
