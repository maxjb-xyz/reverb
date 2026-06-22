package wiring

import (
	"context"
	"database/sql"

	"github.com/maxjb-xyz/reverb/internal/coverage"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// coverageCache adapts *db.Queries to coverage.CoverageCache, translating between
// coverage's row types and the generated db.* params/rows. sql.ErrNoRows is mapped
// to Found:false (GetAlbumCoverage) or a zero-value row + nil error (the others),
// so the service's "cache-miss → fetch" path treats absence as a normal miss.
type coverageCache struct{ q *db.Queries }

// NewCoverageCache constructs the persistence adapter for the coverage service.
func NewCoverageCache(q *db.Queries) coverage.CoverageCache { return &coverageCache{q: q} }

var _ coverage.CoverageCache = (*coverageCache)(nil)

func (c *coverageCache) GetArtistExternalMap(ctx context.Context, libraryArtistID, source string) (coverage.ArtistMapRow, error) {
	row, err := c.q.GetArtistExternalMap(ctx, db.GetArtistExternalMapParams{
		LibraryArtistID: libraryArtistID,
		Source:          source,
	})
	if err == sql.ErrNoRows {
		return coverage.ArtistMapRow{}, nil
	}
	if err != nil {
		return coverage.ArtistMapRow{}, err
	}
	return coverage.ArtistMapRow{ExternalArtistID: row.ExternalArtistID, Confidence: row.Confidence}, nil
}

func (c *coverageCache) UpsertArtistExternalMap(ctx context.Context, libraryArtistID, source, externalID string, confidence float64, now int64) error {
	return c.q.UpsertArtistExternalMap(ctx, db.UpsertArtistExternalMapParams{
		LibraryArtistID:  libraryArtistID,
		Source:           source,
		ExternalArtistID: externalID,
		Confidence:       confidence,
		CreatedAt:        now,
	})
}

func (c *coverageCache) GetDiscographyCache(ctx context.Context, source, externalArtistID string) (coverage.DiscoRow, error) {
	row, err := c.q.GetDiscographyCache(ctx, db.GetDiscographyCacheParams{
		Source:           source,
		ExternalArtistID: externalArtistID,
	})
	if err == sql.ErrNoRows {
		return coverage.DiscoRow{}, nil
	}
	if err != nil {
		return coverage.DiscoRow{}, err
	}
	return coverage.DiscoRow{AlbumsJSON: row.AlbumsJson}, nil
}

func (c *coverageCache) UpsertDiscographyCache(ctx context.Context, source, externalArtistID, albumsJSON string, now int64) error {
	return c.q.UpsertDiscographyCache(ctx, db.UpsertDiscographyCacheParams{
		Source:           source,
		ExternalArtistID: externalArtistID,
		AlbumsJson:       albumsJSON,
		FetchedAt:        now,
	})
}

func (c *coverageCache) GetAlbumCoverage(ctx context.Context, source, externalAlbumID string) (coverage.CoverageRow, error) {
	row, err := c.q.GetAlbumCoverage(ctx, db.GetAlbumCoverageParams{
		Source:          source,
		ExternalAlbumID: externalAlbumID,
	})
	if err == sql.ErrNoRows {
		return coverage.CoverageRow{Found: false}, nil
	}
	if err != nil {
		return coverage.CoverageRow{}, err
	}
	return coverage.CoverageRow{
		CoverageJSON:   row.CoverageJson,
		LibraryAlbumID: row.LibraryAlbumID,
		Found:          true,
	}, nil
}

func (c *coverageCache) UpsertAlbumCoverage(ctx context.Context, source, externalAlbumID, coverageJSON, libraryAlbumID string, now int64) error {
	return c.q.UpsertAlbumCoverage(ctx, db.UpsertAlbumCoverageParams{
		Source:          source,
		ExternalAlbumID: externalAlbumID,
		CoverageJson:    coverageJSON,
		LibraryAlbumID:  libraryAlbumID,
		FetchedAt:       now,
	})
}

func (c *coverageCache) DeleteAlbumCoverageForLibraryAlbum(ctx context.Context, libraryAlbumID string) error {
	return c.q.DeleteAlbumCoverageForLibraryAlbum(ctx, libraryAlbumID)
}

// BuildCoverageService constructs a *coverage.Service from the built services: the
// first enabled search source implementing coverage.DiscoSource (spotify does),
// the library adapter, a matching.Service over the same library, the cache adapter,
// and nowFn. It returns nil when there is no DiscoSource-capable source or no
// library — coverage needs both an external discography source and a library to
// match against. The API handlers return 503 when the service is nil.
func (b *Builder) BuildCoverageService(
	sources []search.SearchSource,
	lib library.LibraryAdapter,
	nowFn func() int64,
) *coverage.Service {
	if lib == nil {
		return nil
	}
	var src coverage.DiscoSource
	for _, s := range sources {
		if ds, ok := s.(coverage.DiscoSource); ok {
			src = ds
			break
		}
	}
	if src == nil {
		return nil
	}
	matcher := matching.NewService(lib, b.queries, b.version.LibraryVersion)
	cache := NewCoverageCache(b.queries)
	return coverage.NewService(src, matcher, lib, cache, nowFn)
}
