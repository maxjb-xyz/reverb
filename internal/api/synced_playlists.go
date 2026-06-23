package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
)

// SyncService is the slice of *playlistsync.Service the API needs.
// *playlistsync.Service satisfies it. It may be nil when no library adapter is
// configured, in which case handlers 503. When a library is configured but no
// Spotify source is present, Spotify-only methods return ErrSpotifyNotConfigured.
type SyncService interface {
	Import(ctx context.Context, url string, downloadMissing bool) (core.SyncedPlaylistDetail, error)
	ImportOnce(ctx context.Context, url string) (core.SyncedPlaylistDetail, error)
	CreateManaged(ctx context.Context, name string) (core.SyncedPlaylistDetail, error)
	List(ctx context.Context) ([]core.SyncedPlaylist, error)
	Detail(ctx context.Context, id string) (core.SyncedPlaylistDetail, error)
	Sync(ctx context.Context, id string) (core.SyncedPlaylistDetail, error)
	DownloadMissing(ctx context.Context, id string) ([]core.DownloadJob, error)
	UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error
	Delete(ctx context.Context, id string) error
	AddTrack(ctx context.Context, id string, entry core.ExternalResult) (core.SyncedPlaylistDetail, error)
	RemoveTrack(ctx context.Context, id, source, externalID string) (core.SyncedPlaylistDetail, error)
}

// sync returns the currently active synced-playlist service under the read lock.
// It may be nil when no PlaylistProvider-capable source is configured.
func (s *Server) sync() SyncService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.sync
}

// importSyncedBody is the POST /synced-playlists request DTO.
type importSyncedBody struct {
	URL             string `json:"url"`
	DownloadMissing bool   `json:"downloadMissing"`
}

// importOnceBody is the POST /playlists/import request DTO.
type importOnceBody struct {
	URL string `json:"url"`
}

// handleImportPlaylistOnce imports a Spotify playlist as a one-time editable managed snapshot.
// POST /api/v1/playlists/import  body {"url":"..."} → 200 core.SyncedPlaylistDetail
func (s *Server) handleImportPlaylistOnce(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	var body importOnceBody
	if err := decode(r, &body); err != nil || body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	det, err := svc.ImportOnce(r.Context(), body.URL)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, playlistsync.ErrNotPlaylistURL) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, playlistsync.ErrSpotifyNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleImportSyncedPlaylist(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	var body importSyncedBody
	if err := decode(r, &body); err != nil || body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	det, err := svc.Import(r.Context(), body.URL, body.DownloadMissing)
	if err != nil {
		// A malformed playlist URL is a bad request; a fetch failure is unprocessable.
		status := http.StatusUnprocessableEntity
		if errors.Is(err, playlistsync.ErrNotPlaylistURL) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, playlistsync.ErrSpotifyNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleListSyncedPlaylists(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	list, err := svc.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list synced playlists"})
		return
	}
	if list == nil {
		list = []core.SyncedPlaylist{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSyncedPlaylistDetail(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	det, err := svc.Detail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleSyncNow(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	det, err := svc.Sync(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, playlistsync.ErrSpotifyNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleSyncedDownloadMissing(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	jobs, err := svc.DownloadMissing(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if jobs == nil {
		jobs = []core.DownloadJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

// syncedSettingsBody is the PUT /synced-playlists/{id}/settings request DTO.
type syncedSettingsBody struct {
	SyncEnabled  bool `json:"syncEnabled"`
	IntervalSec  int  `json:"intervalSec"`
	AutoDownload bool `json:"autoDownload"`
}

func (s *Server) handleSyncedSettings(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	var body syncedSettingsBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if err := svc.UpdateSettings(r.Context(), chi.URLParam(r, "id"), body.SyncEnabled, body.IntervalSec, body.AutoDownload); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteSyncedPlaylist(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	if err := svc.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// addSyncedTrackBody is the POST /synced-playlists/{id}/tracks request DTO.
type addSyncedTrackBody struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	ISRC       string `json:"isrc"`
	DurationMs int    `json:"durationMs"`
}

func (s *Server) handleAddSyncedTrack(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	var body addSyncedTrackBody
	if err := decode(r, &body); err != nil || body.Source == "" || body.ExternalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source and externalId are required"})
		return
	}
	entry := core.ExternalResult{
		Source:     body.Source,
		ExternalID: body.ExternalID,
		Title:      body.Title,
		Artist:     body.Artist,
		Album:      body.Album,
		ISRC:       body.ISRC,
		DurationMs: body.DurationMs,
		Type:       core.EntityTrack,
	}
	det, err := svc.AddTrack(r.Context(), chi.URLParam(r, "id"), entry)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, playlistsync.ErrNotEditable) {
			status = http.StatusConflict // 409 — cannot mutate a synced playlist
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleRemoveSyncedTrack(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	source := r.URL.Query().Get("source")
	externalID := r.URL.Query().Get("externalId")
	if source == "" || externalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source and externalId are required"})
		return
	}
	det, err := svc.RemoveTrack(r.Context(), chi.URLParam(r, "id"), source, externalID)
	if err != nil {
		status := http.StatusUnprocessableEntity
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}
