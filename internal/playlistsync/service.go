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

// ErrNotEditable is returned by AddTrack/RemoveTrack when the playlist is mode='synced'
// (auto-mirrored) and therefore not editable.
var ErrNotEditable = errors.New("playlist is not editable")

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

// LibraryWriter is the library slice the Service needs for one-time imports:
// creating a new editable playlist and bulk-adding tracks.
// *subsonic.LibraryAdapter satisfies this interface.
type LibraryWriter interface {
	CreatePlaylist(ctx context.Context, name string) (core.Playlist, error)
	AddTracksToPlaylist(ctx context.Context, playlistID string, trackIDs []string) error
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
// TrackCount is populated by the List query (via json_array_length in SQL) so the service
// never needs to unmarshal TracksJSON just to count tracks for the list view.
type SyncedRow struct {
	ID, Source, ExternalID, Name, CoverURL, TracksJSON string
	Mode                                                string
	SyncEnabled, AutoDownload                           bool
	SyncIntervalSec                                     int
	LastSyncedAt, CreatedAt                             int64
	TrackCount                                          int // set by List; zero means "not pre-counted, fall back to TracksJSON"
}

type Service struct {
	src   PlaylistSource
	match Matcher
	dl    Downloader
	store Store
	lib   LibraryWriter // optional; nil when no library is configured
	now   func() int64
	newID func() string
}

func NewService(src PlaylistSource, m Matcher, dl Downloader, store Store, lib LibraryWriter, now func() int64, newID func() string) *Service {
	return &Service{src: src, match: m, dl: dl, store: store, lib: lib, now: now, newID: newID}
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
	now := s.now()
	id, err := s.store.Upsert(ctx, core.SyncedPlaylist{
		ID: s.newID(), Source: pl.Source, ExternalID: pl.ExternalID, Name: pl.Name, CoverURL: pl.CoverURL,
	}, string(tj), now)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	// Stamp last_synced_at on import. UpsertSyncedPlaylist only writes created_at, so
	// without this a freshly-imported playlist has last_synced_at=0 and the UI reads
	// "Never synced" until the first Sync. An import IS a sync (we just fetched the
	// live tracklist), so record it as "synced just now". Mirrors the Sync path's
	// UpdateTracks stamping; the tracklist/name/cover written here match the upsert.
	if uErr := s.store.UpdateTracks(ctx, id, pl.Name, pl.CoverURL, string(tj), now); uErr != nil {
		return core.SyncedPlaylistDetail{}, uErr
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
		dt := core.AlbumDetailTrack{Title: tr.Title, Artist: tr.Artist, Album: tr.Album, TrackNumber: i + 1, DurationMs: tr.DurationMs, CoverURL: tr.CoverURL,
			ArtistExternalID: tr.ArtistExternalID, AlbumExternalID: tr.AlbumExternalID}
		if tr.Source == "library" {
			// Directly-added library track: no matching needed — treat as owned.
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{ID: tr.ExternalID, Title: tr.Title, Artist: tr.Artist, Album: tr.Album, DurationMs: tr.DurationMs}
		} else {
			res, mErr := s.match.Match(ctx, tr)
			if mErr != nil {
				return core.SyncedPlaylistDetail{}, mErr
			}
			if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
				det.OwnedCount++
				dt.State = core.CoverageFull
				dt.LibraryTrack = &core.Track{ID: res.LibraryTrackID, Title: tr.Title, Artist: tr.Artist, Album: tr.Album, DurationMs: tr.DurationMs, ArtistID: res.ArtistID, AlbumID: res.AlbumID, CoverArtID: res.CoverArtID}
			} else {
				dt.State = core.CoverageNone
				ref := core.ExternalTrackRef{Source: tr.Source, ExternalID: tr.ExternalID, Title: tr.Title, Artist: tr.Artist, Album: tr.Album, ISRC: tr.ISRC, DurationMs: tr.DurationMs}
				dt.ExternalRef = &ref
			}
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

// ImportOnce imports a Spotify playlist as a one-time editable managed snapshot.
// Unlike Import (which creates a synced mirror), this creates a mode='once' row:
// not auto-synced, but editable (tracks can be added/removed via AddTrack/RemoveTrack).
// All missing tracks are enqueued for download immediately.
//
// Returns ErrNotPlaylistURL when url is not a recognizable Spotify playlist URL.
func (s *Service) ImportOnce(ctx context.Context, url string) (core.SyncedPlaylistDetail, error) {
	extID, ok := s.src.ParsePlaylistID(url)
	if !ok {
		return core.SyncedPlaylistDetail{}, ErrNotPlaylistURL
	}
	pl, err := s.src.GetPlaylist(ctx, extID)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	tj, _ := json.Marshal(pl.Tracks)
	now := s.now()
	newID := s.newID()
	id, err := s.store.Upsert(ctx, core.SyncedPlaylist{
		ID: newID, Source: pl.Source, ExternalID: pl.ExternalID, Name: pl.Name, CoverURL: pl.CoverURL,
		Mode: "once",
	}, string(tj), now)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	// Stamp last_synced_at = now (import IS a sync).
	if uErr := s.store.UpdateTracks(ctx, id, pl.Name, pl.CoverURL, string(tj), now); uErr != nil {
		return core.SyncedPlaylistDetail{}, uErr
	}
	// Enqueue all missing tracks for download.
	for _, tr := range pl.Tracks {
		res, _ := s.match.Match(ctx, tr)
		if res.Status != core.MatchInLibrary {
			_, _ = s.dl.Enqueue(ctx, core.DownloadRequest{
				Source:     tr.Source,
				ExternalID: tr.ExternalID,
				Artist:     tr.Artist,
				Title:      tr.Title,
				Album:      tr.Album,
				ISRC:       tr.ISRC,
				DurationMs: tr.DurationMs,
			})
		}
	}
	return s.Detail(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]core.SyncedPlaylist, error) {
	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []core.SyncedPlaylist{}
	for _, r := range rows {
		count := r.TrackCount
		if count == 0 && r.TracksJSON != "" {
			// Fallback: row came without a pre-counted TrackCount (e.g. from the
			// in-memory test store), so derive it the old way. The real store adapter
			// always populates TrackCount via json_array_length in SQL.
			var tracks []core.ExternalResult
			_ = json.Unmarshal([]byte(r.TracksJSON), &tracks)
			count = len(tracks)
		}
		out = append(out, rowToSummary(r, count))
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

// AddTrack appends an entry to a mode='once' managed playlist's tracklist.
// Returns ErrNotEditable if the playlist is mode='synced'.
// Deduplicates by source+externalId (no-op if already present).
// Enqueues a download if the entry is not already a library track or matched.
func (s *Service) AddTrack(ctx context.Context, id string, entry core.ExternalResult) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	if row.Mode != "once" {
		return core.SyncedPlaylistDetail{}, ErrNotEditable
	}
	var tracks []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &tracks)
	// Dedupe by source+externalId.
	for _, t := range tracks {
		if t.Source == entry.Source && t.ExternalID == entry.ExternalID {
			return s.Detail(ctx, id)
		}
	}
	tracks = append(tracks, entry)
	tj, _ := json.Marshal(tracks)
	if err := s.store.UpdateTracks(ctx, id, row.Name, row.CoverURL, string(tj), s.now()); err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	// Enqueue download if missing (not a library entry and not matched).
	if entry.Source != "library" {
		res, _ := s.match.Match(ctx, entry)
		if res.Status != core.MatchInLibrary {
			_, _ = s.dl.Enqueue(ctx, core.DownloadRequest{
				Source:     entry.Source,
				ExternalID: entry.ExternalID,
				Artist:     entry.Artist,
				Title:      entry.Title,
				Album:      entry.Album,
				ISRC:       entry.ISRC,
				DurationMs: entry.DurationMs,
			})
		}
	}
	return s.Detail(ctx, id)
}

// RemoveTrack removes an entry from a mode='once' managed playlist's tracklist.
// Returns ErrNotEditable if the playlist is mode='synced'.
func (s *Service) RemoveTrack(ctx context.Context, id, source, externalID string) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	if row.Mode != "once" {
		return core.SyncedPlaylistDetail{}, ErrNotEditable
	}
	var tracks []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &tracks)
	filtered := tracks[:0]
	for _, t := range tracks {
		if !(t.Source == source && t.ExternalID == externalID) {
			filtered = append(filtered, t)
		}
	}
	tj, _ := json.Marshal(filtered)
	if err := s.store.UpdateTracks(ctx, id, row.Name, row.CoverURL, string(tj), s.now()); err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	return s.Detail(ctx, id)
}

func rowToSummary(r SyncedRow, trackCount int) core.SyncedPlaylist {
	return core.SyncedPlaylist{
		ID: r.ID, Source: r.Source, ExternalID: r.ExternalID, Name: r.Name, CoverURL: r.CoverURL,
		Mode: r.Mode,
		SyncEnabled: r.SyncEnabled, SyncIntervalSec: r.SyncIntervalSec, AutoDownload: r.AutoDownload,
		LastSyncedAt: r.LastSyncedAt, TrackCount: trackCount,
	}
}
