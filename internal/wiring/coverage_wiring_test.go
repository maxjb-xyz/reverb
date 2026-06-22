package wiring

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// discoSource is a stubSource that also implements coverage.DiscoSource via
// GetArtist + GetArtistDiscography (the optional capabilities the spotify adapter provides).
type discoSource struct{ stubSource }

func (d *discoSource) GetArtist(ctx context.Context, id string) (core.ExternalArtist, error) {
	return core.ExternalArtist{}, nil
}

func (d *discoSource) GetArtistDiscography(ctx context.Context, id string) ([]core.ExternalAlbum, error) {
	return nil, nil
}

func TestBuildCoverageServiceWithDiscoSource(t *testing.T) {
	st := newTestStore(t)
	b := newTestBuilder(t, st)

	sources := []search.SearchSource{&discoSource{}}
	var lib library.LibraryAdapter = &stubLib{}

	cov := b.BuildCoverageService(sources, lib, func() int64 { return 0 })
	if cov == nil {
		t.Fatal("expected a coverage service when a DiscoSource and a library are present")
	}
}

func TestBuildCoverageServiceNilWithoutDiscoSource(t *testing.T) {
	st := newTestStore(t)
	b := newTestBuilder(t, st)

	// stubSource implements SearchSource but NOT GetArtistDiscography → no DiscoSource.
	sources := []search.SearchSource{&stubSource{}}
	var lib library.LibraryAdapter = &stubLib{}

	if cov := b.BuildCoverageService(sources, lib, func() int64 { return 0 }); cov != nil {
		t.Fatal("expected nil coverage service when no source implements DiscoSource")
	}
}

func TestBuildCoverageServiceNilWithoutLibrary(t *testing.T) {
	st := newTestStore(t)
	b := newTestBuilder(t, st)

	sources := []search.SearchSource{&discoSource{}}
	if cov := b.BuildCoverageService(sources, nil, func() int64 { return 0 }); cov != nil {
		t.Fatal("expected nil coverage service when no library is configured")
	}
}

// TestCoverageCacheMissMapsToZero proves the adapter translates a no-rows lookup
// into a non-error miss: GetAlbumCoverage → Found:false, and the cache-row getters
// return zero values with nil error (so the service's fetch path runs).
func TestCoverageCacheMissMapsToZero(t *testing.T) {
	st := newTestStore(t)
	cache := NewCoverageCache(st.Q())
	ctx := context.Background()

	cov, err := cache.GetAlbumCoverage(ctx, "spotify", "missing")
	if err != nil {
		t.Fatalf("GetAlbumCoverage on miss should not error: %v", err)
	}
	if cov.Found {
		t.Fatal("expected Found:false on a cache miss")
	}

	disco, err := cache.GetDiscographyCache(ctx, "spotify", "missing")
	if err != nil {
		t.Fatalf("GetDiscographyCache on miss should not error: %v", err)
	}
	if disco.AlbumsJSON != "" {
		t.Fatalf("expected empty AlbumsJSON on a miss, got %q", disco.AlbumsJSON)
	}

	m, err := cache.GetArtistExternalMap(ctx, "lib-1", "spotify")
	if err != nil {
		t.Fatalf("GetArtistExternalMap on miss should not error: %v", err)
	}
	if m.ExternalArtistID != "" {
		t.Fatalf("expected empty ExternalArtistID on a miss, got %q", m.ExternalArtistID)
	}
}

// TestCoverageCacheRoundTrip proves an upsert is readable back through the adapter,
// including that library_version is stored and returned correctly.
func TestCoverageCacheRoundTrip(t *testing.T) {
	st := newTestStore(t)
	cache := NewCoverageCache(st.Q())
	ctx := context.Background()

	if err := cache.UpsertAlbumCoverage(ctx, "spotify", "alb-1", `{"state":"full"}`, "lib-alb-9", 7, 123); err != nil {
		t.Fatal(err)
	}
	cov, err := cache.GetAlbumCoverage(ctx, "spotify", "alb-1")
	if err != nil {
		t.Fatal(err)
	}
	if !cov.Found || cov.CoverageJSON != `{"state":"full"}` || cov.LibraryAlbumID != "lib-alb-9" || cov.LibraryVersion != 7 {
		t.Fatalf("round-trip mismatch: %+v", cov)
	}
}
