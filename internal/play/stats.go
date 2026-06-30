package play

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

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

// TimeBucket is one time-bucket in a timeline response.
type TimeBucket struct {
	Start    int64
	Plays    int
	MsPlayed int64
}

// ClockCell is one cell in a 7×24 weekday/hour heatmap.
// Weekday 0=Sunday … 6=Saturday; Hour 0–23.
type ClockCell struct {
	Weekday  int
	Hour     int
	Plays    int
	MsPlayed int64
}

// RecentRow is a single row in the recent-plays feed.
type RecentRow struct {
	ID        string
	CatalogID string
	Title     string
	Artist    string
	Album     string
	PlayedAt  int64
}

// EntityStats holds aggregate statistics for a single artist or track.
type EntityStats struct {
	Plays       int
	MsPlayed    int64
	FirstPlayed int64
	LastPlayed  int64
	TopTracks   []TopRow
}

// StatsQuerier is the narrow interface of generated query methods that Stats
// needs. *db.Queries satisfies it.
type StatsQuerier interface {
	StatsSummary(ctx context.Context, arg db.StatsSummaryParams) (db.StatsSummaryRow, error)
	StatsTopTracks(ctx context.Context, arg db.StatsTopTracksParams) ([]db.StatsTopTracksRow, error)
	StatsTopArtists(ctx context.Context, arg db.StatsTopArtistsParams) ([]db.StatsTopArtistsRow, error)
	StatsTopAlbums(ctx context.Context, arg db.StatsTopAlbumsParams) ([]db.StatsTopAlbumsRow, error)
	StatsPlaysInWindow(ctx context.Context, arg db.StatsPlaysInWindowParams) ([]db.StatsPlaysInWindowRow, error)
	ListRecentPlays(ctx context.Context, arg db.ListRecentPlaysParams) ([]db.ListRecentPlaysRow, error)
	StatsEntityArtist(ctx context.Context, arg db.StatsEntityArtistParams) (db.StatsEntityArtistRow, error)
	StatsEntityAlbum(ctx context.Context, arg db.StatsEntityAlbumParams) (db.StatsEntityAlbumRow, error)
	StatsEntityTrack(ctx context.Context, arg db.StatsEntityTrackParams) (db.StatsEntityTrackRow, error)
	StatsTopTracksByArtist(ctx context.Context, arg db.StatsTopTracksByArtistParams) ([]db.StatsTopTracksByArtistRow, error)
	StatsTopTracksByAlbum(ctx context.Context, arg db.StatsTopTracksByAlbumParams) ([]db.StatsTopTracksByAlbumRow, error)
	StatsTopTracksByCatalogID(ctx context.Context, arg db.StatsTopTracksByCatalogIDParams) ([]db.StatsTopTracksByCatalogIDRow, error)
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

// interfaceToInt64 safely converts an interface{} from sqlc (SQLite aggregate
// results like COALESCE/MIN/MAX) to int64. SQLite returns int64 or float64.
func interfaceToInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

// Timeline returns play counts and ms played bucketed into day, week, or month
// UTC buckets for the given [from, to) window, in ascending Start order.
// Only non-empty buckets are returned.
func (s *Stats) Timeline(ctx context.Context, userID string, from, to int64, bucket string) ([]TimeBucket, error) {
	plays, err := s.q.StatsPlaysInWindow(ctx, db.StatsPlaysInWindowParams{
		UserID:     userID,
		PlayedAt:   from,
		PlayedAt_2: to,
	})
	if err != nil {
		return nil, err
	}

	buckets := map[int64]*TimeBucket{}
	for _, p := range plays {
		start := bucketStart(p.PlayedAt, bucket)
		b, ok := buckets[start]
		if !ok {
			b = &TimeBucket{Start: start}
			buckets[start] = b
		}
		b.Plays++
		b.MsPlayed += p.MsPlayed
	}

	out := make([]TimeBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out, nil
}

// bucketStart returns the UTC bucket boundary for a unix-second timestamp.
// bucket must be "day", "week", or "month"; defaults to "day".
func bucketStart(playedAt int64, bucket string) int64 {
	t := time.Unix(playedAt, 0).UTC()
	switch bucket {
	case "week":
		// Monday-anchored: find the Monday 00:00 UTC of this day.
		wd := int(t.Weekday()) // 0=Sun … 6=Sat
		// Days since Monday: Mon=0, Tue=1, … Sun=6
		daysSinceMonday := (wd + 6) % 7
		monday := t.AddDate(0, 0, -daysSinceMonday)
		y, m, d := monday.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
	case "month":
		y, m, _ := t.Date()
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).Unix()
	default: // "day"
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
	}
}

// Clock returns non-empty cells of a 7×24 weekday/hour heatmap.
// Weekday 0=Sunday … 6=Saturday. tzOffsetMin shifts played_at into local time.
func (s *Stats) Clock(ctx context.Context, userID string, from, to int64, tzOffsetMin int) ([]ClockCell, error) {
	plays, err := s.q.StatsPlaysInWindow(ctx, db.StatsPlaysInWindowParams{
		UserID:     userID,
		PlayedAt:   from,
		PlayedAt_2: to,
	})
	if err != nil {
		return nil, err
	}

	type cellKey struct{ weekday, hour int }
	grid := map[cellKey]*ClockCell{}
	for _, p := range plays {
		localSec := p.PlayedAt + int64(tzOffsetMin)*60
		weekday := int((localSec/86400 + 4) % 7)
		hour := int((localSec % 86400) / 3600)
		k := cellKey{weekday, hour}
		c, ok := grid[k]
		if !ok {
			c = &ClockCell{Weekday: weekday, Hour: hour}
			grid[k] = c
		}
		c.Plays++
		c.MsPlayed += p.MsPlayed
	}

	out := make([]ClockCell, 0, len(grid))
	for _, c := range grid {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weekday != out[j].Weekday {
			return out[i].Weekday < out[j].Weekday
		}
		return out[i].Hour < out[j].Hour
	})
	return out, nil
}

// Recent returns the most recent plays for userID, returning plays with
// played_at strictly less than before, newest first, limited to limit rows.
func (s *Stats) Recent(ctx context.Context, userID string, before int64, limit int) ([]RecentRow, error) {
	rows, err := s.q.ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   userID,
		PlayedAt: before,
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]RecentRow, len(rows))
	for i, r := range rows {
		out[i] = RecentRow{
			ID:        r.ID,
			CatalogID: r.CatalogID,
			Title:     r.Title,
			Artist:    r.Artist,
			Album:     r.Album,
			PlayedAt:  r.PlayedAt,
		}
	}
	return out, nil
}

// Entity returns aggregate statistics for a single artist, album, or track.
// kind must be "artist", "album", or "track". id is the artist name (artist),
// the album name (album), or the catalog_id (track). artist is used ONLY for
// kind="album" (an album is identified by NAME + ARTIST because album titles
// collide across artists); it is ignored for kind="artist" and kind="track".
func (s *Stats) Entity(ctx context.Context, userID, kind, id, artist string, from, to int64) (EntityStats, error) {
	const topLimit = 5
	switch kind {
	case "artist":
		row, err := s.q.StatsEntityArtist(ctx, db.StatsEntityArtistParams{
			UserID:     userID,
			Artist:     id,
			PlayedAt:   from,
			PlayedAt_2: to,
		})
		if err != nil {
			return EntityStats{}, err
		}
		topRows, err := s.q.StatsTopTracksByArtist(ctx, db.StatsTopTracksByArtistParams{
			UserID:     userID,
			Artist:     id,
			PlayedAt:   from,
			PlayedAt_2: to,
			Limit:      topLimit,
		})
		if err != nil {
			return EntityStats{}, err
		}
		return EntityStats{
			Plays:       int(row.Plays),
			MsPlayed:    interfaceToInt64(row.MsPlayed),
			FirstPlayed: interfaceToInt64(row.FirstPlayed),
			LastPlayed:  interfaceToInt64(row.LastPlayed),
			TopTracks:   topTracksByArtistToTopRow(topRows),
		}, nil

	case "album":
		// An album is identified by album NAME (id) + artist NAME, because album
		// titles collide across artists.
		row, err := s.q.StatsEntityAlbum(ctx, db.StatsEntityAlbumParams{
			UserID:     userID,
			Album:      id,
			Artist:     artist,
			PlayedAt:   from,
			PlayedAt_2: to,
		})
		if err != nil {
			return EntityStats{}, err
		}
		topRows, err := s.q.StatsTopTracksByAlbum(ctx, db.StatsTopTracksByAlbumParams{
			UserID:     userID,
			Album:      id,
			Artist:     artist,
			PlayedAt:   from,
			PlayedAt_2: to,
			Limit:      topLimit,
		})
		if err != nil {
			return EntityStats{}, err
		}
		return EntityStats{
			Plays:       int(row.Plays),
			MsPlayed:    interfaceToInt64(row.MsPlayed),
			FirstPlayed: interfaceToInt64(row.FirstPlayed),
			LastPlayed:  interfaceToInt64(row.LastPlayed),
			TopTracks:   topTracksByAlbumToTopRow(topRows),
		}, nil

	case "track":
		row, err := s.q.StatsEntityTrack(ctx, db.StatsEntityTrackParams{
			UserID:     userID,
			CatalogID:  id,
			PlayedAt:   from,
			PlayedAt_2: to,
		})
		if err != nil {
			return EntityStats{}, err
		}
		topRows, err := s.q.StatsTopTracksByCatalogID(ctx, db.StatsTopTracksByCatalogIDParams{
			UserID:     userID,
			CatalogID:  id,
			PlayedAt:   from,
			PlayedAt_2: to,
			Limit:      topLimit,
		})
		if err != nil {
			return EntityStats{}, err
		}
		return EntityStats{
			Plays:       int(row.Plays),
			MsPlayed:    interfaceToInt64(row.MsPlayed),
			FirstPlayed: interfaceToInt64(row.FirstPlayed),
			LastPlayed:  interfaceToInt64(row.LastPlayed),
			TopTracks:   topTracksByCatalogIDToTopRow(topRows),
		}, nil

	default:
		return EntityStats{}, fmt.Errorf("entity kind %q unknown; must be artist, album, or track", kind)
	}
}

func topTracksByArtistToTopRow(rows []db.StatsTopTracksByArtistRow) []TopRow {
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
	return out
}

func topTracksByAlbumToTopRow(rows []db.StatsTopTracksByAlbumRow) []TopRow {
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
	return out
}

func topTracksByCatalogIDToTopRow(rows []db.StatsTopTracksByCatalogIDRow) []TopRow {
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
	return out
}
