package playlistsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// ErrNotPlaylistURL is returned by Import when the supplied URL is not a
// recognizable Spotify playlist URL (a client error, not a fetch failure).
var ErrNotPlaylistURL = errors.New("not a spotify playlist url")

// ErrNotEditable is returned by AddTrack/RemoveTrack when the playlist is mode='synced'
// (auto-mirrored) and therefore not editable.
var ErrNotEditable = errors.New("playlist is not editable")

// ErrSpotifyNotConfigured is returned by Import, ImportOnce, and Sync when no
// Spotify PlaylistSource has been configured. Managed-playlist operations
// (CreateManaged, List, Detail, AddTrack, RemoveTrack) work without Spotify.
var ErrSpotifyNotConfigured = errors.New("spotify is not configured")

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

// LibraryReader is the library slice needed for migrating existing Navidrome
// playlists into managed playlists. *subsonic.LibraryAdapter satisfies this.
type LibraryReader interface {
	GetPlaylists(ctx context.Context) ([]core.Playlist, error)
	GetPlaylist(ctx context.Context, id string) (core.Playlist, error)
}

// SettingsStore is the key/value settings persistence used for migration guards.
// *db.Queries satisfies this via GetSetting/UpsertSetting.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, key, value string) error
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
	src      PlaylistSource
	match    Matcher
	dl       Downloader
	store    Store
	lib      LibraryWriter // optional; nil when no library is configured
	libRead  LibraryReader // optional; for migration
	settings SettingsStore // optional; for migration flag guard
	now      func() int64
	newID    func() string
}

func NewService(src PlaylistSource, m Matcher, dl Downloader, store Store, lib LibraryWriter, now func() int64, newID func() string) *Service {
	return &Service{src: src, match: m, dl: dl, store: store, lib: lib, now: now, newID: newID}
}

// WithLibraryReader attaches a LibraryReader so MigrateLibraryPlaylists can read
// existing Navidrome playlists. Returns the receiver for chaining.
func (s *Service) WithLibraryReader(r LibraryReader) *Service {
	s.libRead = r
	return s
}

// WithSettingsStore attaches a SettingsStore so MigrateLibraryPlaylists can
// read/write the migration flag. Returns the receiver for chaining.
func (s *Service) WithSettingsStore(ss SettingsStore) *Service {
	s.settings = ss
	return s
}

func (s *Service) Import(ctx context.Context, rawURL string, downloadMissing bool) (core.SyncedPlaylistDetail, error) {
	if s.src == nil {
		return core.SyncedPlaylistDetail{}, ErrSpotifyNotConfigured
	}
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
			// CoverArtID is carried from the stored entry so migrated tracks show covers.
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{
				ID:         tr.ExternalID,
				Title:      tr.Title,
				Artist:     tr.Artist,
				Album:      tr.Album,
				DurationMs: tr.DurationMs,
				CoverArtID: tr.CoverArtID,
			}
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
	if s.src == nil {
		return core.SyncedPlaylistDetail{}, ErrSpotifyNotConfigured
	}
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
	if s.src == nil {
		return core.SyncedPlaylistDetail{}, ErrSpotifyNotConfigured
	}
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

// CreateManaged creates a blank, locally-managed playlist (source="local",
// mode="once", empty tracks). It does NOT touch Navidrome/Subsonic.
func (s *Service) CreateManaged(ctx context.Context, name string) (core.SyncedPlaylistDetail, error) {
	id := s.newID()
	now := s.now()
	storedID, err := s.store.Upsert(ctx, core.SyncedPlaylist{
		ID:         id,
		Source:     "local",
		ExternalID: id, // UNIQUE(source,external_id) — reuse the generated id
		Name:       name,
		CoverURL:   "",
		Mode:       "once",
	}, "[]", now)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	// Stamp last_synced_at so the UI doesn't show "Never synced" on a brand-new playlist.
	if uErr := s.store.UpdateTracks(ctx, storedID, name, "", "[]", now); uErr != nil {
		return core.SyncedPlaylistDetail{}, uErr
	}
	return s.Detail(ctx, storedID)
}

const migrationKey = "navidrome_playlists_migrated"

// MigrateLibraryPlaylists copies every Navidrome/Subsonic playlist into the
// synced_playlists table as source="local", mode="once" managed playlists.
// Guarded by a settings flag so it runs ONCE; subsequent calls are no-ops.
// Requires WithLibraryReader and WithSettingsStore to have been called;
// silently returns (no-op) when either is nil.
func (s *Service) MigrateLibraryPlaylists(ctx context.Context) error {
	if s.libRead == nil || s.settings == nil {
		return nil
	}
	// Check flag.
	val, _ := s.settings.GetSetting(ctx, migrationKey)
	if val == "true" {
		return nil
	}
	playlists, err := s.libRead.GetPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("migrate library playlists: list: %w", err)
	}
	migrated := 0
	for _, pl := range playlists {
		full, err := s.libRead.GetPlaylist(ctx, pl.ID)
		if err != nil {
			log.Printf("migrate library playlists: GetPlaylist(%q): %v — skipping", pl.ID, err)
			continue
		}
		tracks := make([]core.ExternalResult, 0, len(full.Tracks))
		for _, tr := range full.Tracks {
			tracks = append(tracks, core.ExternalResult{
				Source:     "library",
				ExternalID: tr.ID,
				Title:      tr.Title,
				Artist:     tr.Artist,
				Album:      tr.Album,
				ISRC:       tr.ISRC,
				DurationMs: tr.DurationMs,
				CoverArtID: tr.CoverArtID,
				Type:       core.EntityTrack,
			})
		}
		tj, _ := json.Marshal(tracks)
		newID := s.newID()
		now := s.now()
		storedID, err := s.store.Upsert(ctx, core.SyncedPlaylist{
			ID:         newID,
			Source:     "local",
			ExternalID: newID,
			Name:       full.Name,
			CoverURL:   "",
			Mode:       "once",
		}, string(tj), now)
		if err != nil {
			log.Printf("migrate library playlists: upsert %q: %v — skipping", full.Name, err)
			continue
		}
		if uErr := s.store.UpdateTracks(ctx, storedID, full.Name, "", string(tj), now); uErr != nil {
			log.Printf("migrate library playlists: UpdateTracks %q: %v — skipping", full.Name, uErr)
			continue
		}
		migrated++
	}
	// Set flag regardless of partial errors so a subsequent restart doesn't re-run.
	if sErr := s.settings.UpsertSetting(ctx, migrationKey, "true"); sErr != nil {
		log.Printf("migrate library playlists: set flag: %v", sErr)
	}
	log.Printf("migrate library playlists: migrated %d library playlist(s)", migrated)
	return nil
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

// SetCover updates the cover_url for a mode='once' playlist.
// Returns ErrNotEditable when the playlist is mode='synced'.
func (s *Service) SetCover(ctx context.Context, id, coverURL string) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	if row.Mode != "once" {
		return core.SyncedPlaylistDetail{}, ErrNotEditable
	}
	if err := s.store.UpdateTracks(ctx, id, row.Name, coverURL, row.TracksJSON, s.now()); err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	return s.Detail(ctx, id)
}

// ReorderTracks reorders a mode='once' playlist's tracklist to match order.
// Entries in order that don't exist in the tracklist are ignored.
// Entries in the tracklist not found in order are appended at the end in their original relative order.
// Returns ErrNotEditable when the playlist is mode='synced'.
func (s *Service) ReorderTracks(ctx context.Context, id string, order []core.TrackKey) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	if row.Mode != "once" {
		return core.SyncedPlaylistDetail{}, ErrNotEditable
	}
	var tracks []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &tracks)

	// Build a lookup: (source, externalID) → track entry.
	type key struct{ source, externalID string }
	byKey := make(map[key]core.ExternalResult, len(tracks))
	for _, t := range tracks {
		byKey[key{t.Source, t.ExternalID}] = t
	}

	// Build the reordered list: first tracks that appear in order (in that order),
	// then remaining tracks in their original relative order.
	inOrder := make(map[key]bool, len(order))
	reordered := make([]core.ExternalResult, 0, len(tracks))
	for _, k := range order {
		tk := key{k.Source, k.ExternalID}
		if t, ok := byKey[tk]; ok {
			reordered = append(reordered, t)
			inOrder[tk] = true
		}
	}
	for _, t := range tracks {
		if !inOrder[key{t.Source, t.ExternalID}] {
			reordered = append(reordered, t)
		}
	}

	tj, _ := json.Marshal(reordered)
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
