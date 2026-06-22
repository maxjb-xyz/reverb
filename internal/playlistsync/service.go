package playlistsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// ErrNotPlaylistURL is returned by Import when the supplied URL is not a
// recognizable Spotify playlist URL (a client error, not a fetch failure).
var ErrNotPlaylistURL = errors.New("not a spotify playlist url")

type PlaylistSource interface {
	ParsePlaylistID(url string) (string, bool)
	GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error)
}
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}
type Downloader interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
}
type Store interface {
	Upsert(ctx context.Context, p core.SyncedPlaylist, tracksJSON string, createdAt int64) (string, error) // returns id
	Get(ctx context.Context, id string) (row SyncedRow, err error)
	List(ctx context.Context) ([]SyncedRow, error)
	ListDue(ctx context.Context, now int64) ([]SyncedRow, error)
	UpdateTracks(ctx context.Context, id, name, coverURL, tracksJSON string, lastSyncedAt int64) error
	UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error
	Delete(ctx context.Context, id string) error
}

// SyncedRow is the store's row shape (decoupled from db.*; the wiring adapter maps db rows to this).
type SyncedRow struct {
	ID, Source, ExternalID, Name, CoverURL, TracksJSON string
	SyncEnabled, AutoDownload                          bool
	SyncIntervalSec                                    int
	LastSyncedAt, CreatedAt                            int64
}

type Service struct {
	src   PlaylistSource
	match Matcher
	dl    Downloader
	store Store
	now   func() int64
	newID func() string
}

func NewService(src PlaylistSource, m Matcher, dl Downloader, store Store, now func() int64, newID func() string) *Service {
	return &Service{src: src, match: m, dl: dl, store: store, now: now, newID: newID}
}

func (s *Service) Import(ctx context.Context, rawURL string, downloadMissing bool) (core.SyncedPlaylistDetail, error) {
	extID, ok := s.src.ParsePlaylistID(rawURL)
	if !ok {
		return core.SyncedPlaylistDetail{}, ErrNotPlaylistURL
	}
	pl, err := s.src.GetPlaylist(ctx, extID)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	tj, _ := json.Marshal(pl.Tracks)
	id, err := s.store.Upsert(ctx, core.SyncedPlaylist{
		ID: s.newID(), Source: pl.Source, ExternalID: pl.ExternalID, Name: pl.Name, CoverURL: pl.CoverURL,
	}, string(tj), s.now())
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	det, err := s.Detail(ctx, id)
	if err != nil {
		return det, err
	}
	if downloadMissing {
		s.enqueueMissing(ctx, det)
	}
	return det, nil
}

func (s *Service) Detail(ctx context.Context, id string) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	var tracks []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &tracks)
	det := core.SyncedPlaylistDetail{SyncedPlaylist: rowToSummary(row, len(tracks))}
	det.TotalCount = len(tracks)
	for i, tr := range tracks {
		res, mErr := s.match.Match(ctx, tr)
		if mErr != nil {
			return core.SyncedPlaylistDetail{}, mErr
		}
		dt := core.AlbumDetailTrack{Title: tr.Title, Artist: tr.Artist, TrackNumber: i + 1, DurationMs: tr.DurationMs, CoverURL: tr.CoverURL}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{ID: res.LibraryTrackID, Title: tr.Title, Artist: tr.Artist, DurationMs: tr.DurationMs}
		} else {
			dt.State = core.CoverageNone
			ref := core.ExternalTrackRef{Source: tr.Source, ExternalID: tr.ExternalID, Title: tr.Title, Artist: tr.Artist, Album: tr.Album, ISRC: tr.ISRC, DurationMs: tr.DurationMs}
			dt.ExternalRef = &ref
		}
		det.Tracks = append(det.Tracks, dt)
	}
	return det, nil
}

func (s *Service) Sync(ctx context.Context, id string) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	pl, err := s.src.GetPlaylist(ctx, row.ExternalID)
	if err != nil {
		// Keep the last-known tracklist; surface the error.
		return core.SyncedPlaylistDetail{}, fmt.Errorf("sync %s: %w", id, err)
	}
	tj, _ := json.Marshal(pl.Tracks)
	if err := s.store.UpdateTracks(ctx, id, pl.Name, pl.CoverURL, string(tj), s.now()); err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	det, err := s.Detail(ctx, id)
	if err != nil {
		return det, err
	}
	if row.AutoDownload {
		s.enqueueMissing(ctx, det)
	}
	return det, nil
}

func (s *Service) enqueueMissing(ctx context.Context, det core.SyncedPlaylistDetail) {
	for _, t := range det.Tracks {
		if t.State == core.CoverageNone && t.ExternalRef != nil {
			_, _ = s.dl.Enqueue(ctx, core.DownloadRequest{
				Source: t.ExternalRef.Source, ExternalID: t.ExternalRef.ExternalID, Artist: t.ExternalRef.Artist,
				Title: t.ExternalRef.Title, Album: t.ExternalRef.Album, ISRC: t.ExternalRef.ISRC, DurationMs: t.ExternalRef.DurationMs,
			})
		}
	}
}

func (s *Service) List(ctx context.Context) ([]core.SyncedPlaylist, error) {
	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []core.SyncedPlaylist{}
	for _, r := range rows {
		var tracks []core.ExternalResult
		_ = json.Unmarshal([]byte(r.TracksJSON), &tracks)
		out = append(out, rowToSummary(r, len(tracks)))
	}
	return out, nil
}

func (s *Service) UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error {
	return s.store.UpdateSettings(ctx, id, enabled, intervalSec, autoDownload)
}
func (s *Service) Delete(ctx context.Context, id string) error { return s.store.Delete(ctx, id) }

// DownloadMissing enqueues the missing tracks for a synced playlist; returns jobs.
func (s *Service) DownloadMissing(ctx context.Context, id string) ([]core.DownloadJob, error) {
	det, err := s.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	jobs := []core.DownloadJob{}
	for _, t := range det.Tracks {
		if t.State == core.CoverageNone && t.ExternalRef != nil {
			j, e := s.dl.Enqueue(ctx, core.DownloadRequest{
				Source: t.ExternalRef.Source, ExternalID: t.ExternalRef.ExternalID, Artist: t.ExternalRef.Artist,
				Title: t.ExternalRef.Title, Album: t.ExternalRef.Album, ISRC: t.ExternalRef.ISRC, DurationMs: t.ExternalRef.DurationMs,
			})
			if e == nil {
				jobs = append(jobs, j)
			}
		}
	}
	return jobs, nil
}

func rowToSummary(r SyncedRow, trackCount int) core.SyncedPlaylist {
	return core.SyncedPlaylist{
		ID: r.ID, Source: r.Source, ExternalID: r.ExternalID, Name: r.Name, CoverURL: r.CoverURL,
		SyncEnabled: r.SyncEnabled, SyncIntervalSec: r.SyncIntervalSec, AutoDownload: r.AutoDownload,
		LastSyncedAt: r.LastSyncedAt, TrackCount: trackCount,
	}
}
