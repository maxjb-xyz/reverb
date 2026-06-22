package wiring

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/search/spotify"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// syncStore adapts *db.Queries to playlistsync.Store, mapping db.SyncedPlaylist
// rows ↔ playlistsync.SyncedRow (bool ↔ int64 0/1; tracks_json passthrough).
type syncStore struct{ q *db.Queries }

// NewSyncStore constructs the persistence adapter for the playlist-sync service.
func NewSyncStore(q *db.Queries) playlistsync.Store { return &syncStore{q: q} }

var _ playlistsync.Store = (*syncStore)(nil)

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func rowToSync(r db.SyncedPlaylist) playlistsync.SyncedRow {
	return playlistsync.SyncedRow{
		ID:              r.ID,
		Source:          r.Source,
		ExternalID:      r.ExternalID,
		Name:            r.Name,
		CoverURL:        r.CoverUrl,
		TracksJSON:      r.TracksJson,
		SyncEnabled:     r.SyncEnabled != 0,
		AutoDownload:    r.AutoDownload != 0,
		SyncIntervalSec: int(r.SyncIntervalSec),
		LastSyncedAt:    r.LastSyncedAt,
		CreatedAt:       r.CreatedAt,
	}
}

// Upsert inserts the playlist, or updates name/cover/tracks on a
// (source, external_id) conflict. To make re-import idempotent end-to-end it
// first looks up any existing row by (source, external_id) and reuses its id —
// the generated id from p.ID is only used for a genuinely new row, so the
// caller always gets back the canonical (existing) id on conflict.
func (s *syncStore) Upsert(ctx context.Context, p core.SyncedPlaylist, tracksJSON string, createdAt int64) (string, error) {
	id := p.ID
	existing, err := s.q.GetSyncedPlaylistBySource(ctx, db.GetSyncedPlaylistBySourceParams{
		Source:     p.Source,
		ExternalID: p.ExternalID,
	})
	if err == nil {
		id = existing.ID
	} else if err != sql.ErrNoRows {
		return "", err
	}
	row, err := s.q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
		ID:         id,
		Source:     p.Source,
		ExternalID: p.ExternalID,
		Name:       p.Name,
		CoverUrl:   p.CoverURL,
		TracksJson: tracksJSON,
		CreatedAt:  createdAt,
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

func (s *syncStore) Get(ctx context.Context, id string) (playlistsync.SyncedRow, error) {
	row, err := s.q.GetSyncedPlaylist(ctx, id)
	if err != nil {
		return playlistsync.SyncedRow{}, err
	}
	return rowToSync(row), nil
}

func (s *syncStore) List(ctx context.Context) ([]playlistsync.SyncedRow, error) {
	rows, err := s.q.ListSyncedPlaylists(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]playlistsync.SyncedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToSync(r))
	}
	return out, nil
}

func (s *syncStore) ListDue(ctx context.Context, now int64) ([]playlistsync.SyncedRow, error) {
	rows, err := s.q.ListDueSyncedPlaylists(ctx, now)
	if err != nil {
		return nil, err
	}
	out := make([]playlistsync.SyncedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToSync(r))
	}
	return out, nil
}

func (s *syncStore) UpdateTracks(ctx context.Context, id, name, coverURL, tracksJSON string, lastSyncedAt int64) error {
	return s.q.UpdateSyncedPlaylistTracks(ctx, db.UpdateSyncedPlaylistTracksParams{
		Name:         name,
		CoverUrl:     coverURL,
		TracksJson:   tracksJSON,
		LastSyncedAt: lastSyncedAt,
		ID:           id,
	})
}

func (s *syncStore) UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error {
	return s.q.UpdateSyncedPlaylistSettings(ctx, db.UpdateSyncedPlaylistSettingsParams{
		SyncEnabled:     b2i(enabled),
		SyncIntervalSec: int64(intervalSec),
		AutoDownload:    b2i(autoDownload),
		ID:              id,
	})
}

func (s *syncStore) Delete(ctx context.Context, id string) error {
	return s.q.DeleteSyncedPlaylist(ctx, id)
}

// spotifyPlaylistSource wraps a search.PlaylistProvider (GetPlaylist) plus the
// package-level spotify.ParsePlaylistID into a playlistsync.PlaylistSource.
type spotifyPlaylistSource struct{ p search.PlaylistProvider }

func (s spotifyPlaylistSource) ParsePlaylistID(url string) (string, bool) {
	return spotify.ParsePlaylistID(url)
}

func (s spotifyPlaylistSource) GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error) {
	return s.p.GetPlaylist(ctx, externalID)
}

// BuildSyncService constructs a *playlistsync.Service from the built services:
// the first enabled search source implementing search.PlaylistProvider (spotify
// does), a matching.Service over the library, the download Manager, and the
// store adapter over the shared *db.Queries. It returns nil when there is no
// PlaylistProvider-capable source, no library, or no download Manager — sync
// needs a source to fetch from, a library to match against, and a downloader to
// fetch missing tracks. The API handlers return 503 when the service is nil.
func (b *Builder) BuildSyncService(
	sources []search.SearchSource,
	lib library.LibraryAdapter,
	mgr *download.Manager,
) *playlistsync.Service {
	if lib == nil || mgr == nil {
		return nil
	}
	var provider search.PlaylistProvider
	for _, s := range sources {
		if pp, ok := s.(search.PlaylistProvider); ok {
			provider = pp
			break
		}
	}
	if provider == nil {
		return nil
	}
	src := spotifyPlaylistSource{p: provider}
	matcher := matching.NewService(lib, b.queries, b.version.LibraryVersion)
	store := NewSyncStore(b.queries)
	nowUnix := func() int64 { return time.Now().Unix() }
	return playlistsync.NewService(src, matcher, mgr, store, nowUnix, uuid.NewString)
}
