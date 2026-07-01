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
		Mode:            r.Mode,
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
	mode := p.Mode
	if mode == "" {
		mode = "synced"
	}
	row, err := s.q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
		ID:         id,
		Source:     p.Source,
		ExternalID: p.ExternalID,
		Name:       p.Name,
		CoverUrl:   p.CoverURL,
		TracksJson: tracksJSON,
		Mode:       mode,
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
		if err == sql.ErrNoRows {
			return playlistsync.SyncedRow{}, playlistsync.ErrNotFound
		}
		return playlistsync.SyncedRow{}, err
	}
	return rowToSync(row), nil
}

func (s *syncStore) List(ctx context.Context) ([]playlistsync.SyncedRow, error) {
	rows, err := s.q.ListSyncedPlaylistsCount(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]playlistsync.SyncedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, playlistsync.SyncedRow{
			ID:              r.ID,
			Source:          r.Source,
			ExternalID:      r.ExternalID,
			Name:            r.Name,
			CoverURL:        r.CoverUrl,
			SyncEnabled:     r.SyncEnabled != 0,
			AutoDownload:    r.AutoDownload != 0,
			SyncIntervalSec: int(r.SyncIntervalSec),
			LastSyncedAt:    r.LastSyncedAt,
			CreatedAt:       r.CreatedAt,
			TrackCount:      int(r.TrackCount),
			Mode:            r.Mode,
			// TracksJSON intentionally omitted: List only needs the count, which
			// sql computed via json_array_length — no full blob unmarshal needed.
		})
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

// dbSettingsStore adapts *db.Queries to playlistsync.SettingsStore.
type dbSettingsStore struct{ q *db.Queries }

var _ playlistsync.SettingsStore = (*dbSettingsStore)(nil)

func (s *dbSettingsStore) GetSetting(ctx context.Context, key string) (string, error) {
	return s.q.GetSetting(ctx, key)
}
func (s *dbSettingsStore) UpsertSetting(ctx context.Context, key, value string) error {
	return s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: key, Value: value})
}

// BuildSyncService constructs a *playlistsync.Service from the built services.
// It requires a library adapter and a download Manager; both are needed for the
// managed-playlist operations (CreateManaged, List, Detail, AddTrack, RemoveTrack)
// that work without any Spotify source. Returns nil only when the library or
// Manager is absent. When a search source implementing search.PlaylistProvider
// (spotify) is present it is wired in as src; otherwise src is nil and the
// Spotify-only methods (Import, ImportOnce, Sync) return ErrSpotifyNotConfigured
// while all managed-playlist operations continue to work normally.
func (b *Builder) BuildSyncService(
	sources []search.SearchSource,
	lib library.LibraryAdapter,
	mgr *download.Manager,
) *playlistsync.Service {
	if lib == nil || mgr == nil {
		return nil
	}
	var src playlistsync.PlaylistSource // nil when Spotify is not configured
	for _, s := range sources {
		if pp, ok := s.(search.PlaylistProvider); ok {
			src = spotifyPlaylistSource{p: pp}
			break
		}
	}
	matcher := matching.NewService(lib, b.queries, b.version.LibraryVersion)
	store := NewSyncStore(b.queries)
	settings := &dbSettingsStore{q: b.queries}
	nowUnix := func() int64 { return time.Now().Unix() }
	// Wrap the wiring-level resolverProvider into a playlistsync.BindingResolver
	// provider. Nil when SetResolverProvider was not called.
	var syncResolve func() playlistsync.BindingResolver
	if b.resolverProvider != nil {
		rp := b.resolverProvider // capture once
		syncResolve = func() playlistsync.BindingResolver {
			r := rp()
			if r == nil {
				return nil
			}
			return r
		}
	}
	svc := playlistsync.NewService(src, matcher, mgr, store, lib, nowUnix, uuid.NewString, syncResolve)
	svc.WithLibraryReader(lib)
	svc.WithSettingsStore(settings)
	return svc
}
