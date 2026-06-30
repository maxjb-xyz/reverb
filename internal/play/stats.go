package play

import (
	"context"
	"database/sql"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// SummaryStats holds aggregate listening metrics for a user over a time window.
type SummaryStats struct {
	Plays           int
	DistinctTracks  int
	DistinctArtists int
	DistinctAlbums  int
	MsPlayed        int64
}

// TopRow is a single entry in a top-tracks/artists/albums list.
// CatalogID/Title/Album are empty for artist grouping; CatalogID/Title are
// empty for album grouping — only the fields meaningful for each query are set.
type TopRow struct {
	CatalogID string
	Title     string
	Artist    string
	Album     string
	Plays     int
	MsPlayed  int64
}

// StatsQuerier is the narrow interface of generated query methods that Stats
// needs. *db.Queries satisfies it.
type StatsQuerier interface {
	StatsSummary(ctx context.Context, arg db.StatsSummaryParams) (db.StatsSummaryRow, error)
	StatsTopTracks(ctx context.Context, arg db.StatsTopTracksParams) ([]db.StatsTopTracksRow, error)
	StatsTopArtists(ctx context.Context, arg db.StatsTopArtistsParams) ([]db.StatsTopArtistsRow, error)
	StatsTopAlbums(ctx context.Context, arg db.StatsTopAlbumsParams) ([]db.StatsTopAlbumsRow, error)
}

// Stats provides compute-on-read listening statistics.
type Stats struct {
	q StatsQuerier
}

// NewStats constructs a Stats service.
func NewStats(q StatsQuerier) *Stats {
	return &Stats{q: q}
}

// Summary returns aggregate counts and total ms played for userID in [from, to).
// An empty window (no plays) returns all-zero SummaryStats without error.
func (s *Stats) Summary(ctx context.Context, userID string, from, to int64) (SummaryStats, error) {
	row, err := s.q.StatsSummary(ctx, db.StatsSummaryParams{
		UserID:     userID,
		PlayedAt:   from,
		PlayedAt_2: to,
	})
	if err != nil {
		return SummaryStats{}, err
	}

	// MsPlayed is COALESCE(SUM(ms_played), 0) — sqlc types it as interface{}.
	// The sqlite driver returns int64 for integer results; guard against nil/float.
	var msPlayed int64
	switch v := row.MsPlayed.(type) {
	case int64:
		msPlayed = v
	case float64:
		msPlayed = int64(v)
	}

	return SummaryStats{
		Plays:           int(row.Plays),
		DistinctTracks:  int(row.DistinctTracks),
		DistinctArtists: int(row.DistinctArtists),
		DistinctAlbums:  int(row.DistinctAlbums),
		MsPlayed:        msPlayed,
	}, nil
}

// TopTracks returns the top-played tracks for userID in [from, to), limited to
// limit rows ordered by play count descending. CatalogID is always populated.
func (s *Stats) TopTracks(ctx context.Context, userID string, from, to int64, limit int) ([]TopRow, error) {
	rows, err := s.q.StatsTopTracks(ctx, db.StatsTopTracksParams{
		UserID:     userID,
		PlayedAt:   from,
		PlayedAt_2: to,
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]TopRow, len(rows))
	for i, r := range rows {
		out[i] = TopRow{
			CatalogID: r.CatalogID,
			Title:     r.Title,
			Artist:    r.Artist,
			Album:     r.Album,
			Plays:     int(r.Plays),
			MsPlayed:  nullFloat64Int64(r.MsPlayed),
		}
	}
	return out, nil
}

// TopArtists returns the top-played artists for userID in [from, to), limited
// to limit rows. CatalogID, Title, and Album are always empty.
func (s *Stats) TopArtists(ctx context.Context, userID string, from, to int64, limit int) ([]TopRow, error) {
	rows, err := s.q.StatsTopArtists(ctx, db.StatsTopArtistsParams{
		UserID:     userID,
		PlayedAt:   from,
		PlayedAt_2: to,
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]TopRow, len(rows))
	for i, r := range rows {
		out[i] = TopRow{
			Artist:   r.Artist,
			Plays:    int(r.Plays),
			MsPlayed: nullFloat64Int64(r.MsPlayed),
		}
	}
	return out, nil
}

// TopAlbums returns the top-played albums for userID in [from, to), limited to
// limit rows. CatalogID and Title are always empty; Artist is the album artist.
func (s *Stats) TopAlbums(ctx context.Context, userID string, from, to int64, limit int) ([]TopRow, error) {
	rows, err := s.q.StatsTopAlbums(ctx, db.StatsTopAlbumsParams{
		UserID:     userID,
		PlayedAt:   from,
		PlayedAt_2: to,
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]TopRow, len(rows))
	for i, r := range rows {
		out[i] = TopRow{
			Album:    r.Album,
			Artist:   r.Artist,
			Plays:    int(r.Plays),
			MsPlayed: nullFloat64Int64(r.MsPlayed),
		}
	}
	return out, nil
}

// nullFloat64Int64 converts a sql.NullFloat64 (how sqlc types SQLite SUM
// results) to int64, returning 0 for NULL.
func nullFloat64Int64(v sql.NullFloat64) int64 {
	if !v.Valid {
		return 0
	}
	return int64(v.Float64)
}
