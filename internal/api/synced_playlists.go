package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

var validIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validPlaylistID(id string) bool {
	return id != "" && validIDRe.MatchString(id)
}

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
	SetCover(ctx context.Context, id, coverURL string) (core.SyncedPlaylistDetail, error)
	ReorderTracks(ctx context.Context, id string, order []core.TrackKey) (core.SyncedPlaylistDetail, error)
	Rename(ctx context.Context, id, name string) (core.SyncedPlaylistDetail, error)
}

// sync returns the currently active synced-playlist service under the read lock.
// It may be nil when no PlaylistProvider-capable source is configured.
func (s *Server) sync() SyncService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.sync
}

// playlistAccessAllowed reports whether the current user may read or mutate the
// playlist identified by id. Admins bypass ownership (may act on any playlist).
// When no PlaylistOwner store is configured, access is denied (fail-closed) to
// prevent a wiring slip from silently disabling playlist isolation. A non-admin,
// non-owner caller is denied so the handler can 404 without leaking the playlist's
// existence.
func (s *Server) playlistAccessAllowed(r *http.Request, id string) bool {
	store := s.deps.PlaylistOwner
	if store == nil {
		return false
	}
	cu, ok := currentUser(r)
	if !ok {
		return false
	}
	if cu.Has(auth.CapAdmin) {
		return true
	}
	owner, err := store.GetSyncedPlaylistOwner(r.Context(), id)
	if err != nil {
		// Unknown playlist (or read error) → treat as no access; caller 404s.
		return false
	}
	return owner.Valid && owner.String == cu.ID
}

// stampPlaylistOwner assigns the current user as the owner of a freshly created
// playlist. A no-op when no PlaylistOwner store is configured or no user is in
// context. Errors are non-fatal: ownership is best-effort attribution at create
// time and must not fail an otherwise-successful create.
func (s *Server) stampPlaylistOwner(r *http.Request, id string) {
	store := s.deps.PlaylistOwner
	if store == nil || id == "" {
		return
	}
	cu, ok := currentUser(r)
	if !ok || cu.ID == "" {
		return
	}
	_ = store.SetSyncedPlaylistOwner(r.Context(), db.SetSyncedPlaylistOwnerParams{
		OwnerUserID: sql.NullString{String: cu.ID, Valid: true},
		ID:          id,
	})
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
	s.stampPlaylistOwner(r, det.ID)
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
	// Silently strip downloadMissing if the caller lacks auto_approve: the
	// import itself is allowed under CapCreatePlaylists, but auto-triggering
	// downloads requires auto_approve.
	if body.DownloadMissing {
		if cu, ok := currentUser(r); !ok || !cu.Has(auth.CapAutoApprove) {
			body.DownloadMissing = false
		}
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
	s.stampPlaylistOwner(r, det.ID)
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleListSyncedPlaylists(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	// Owner-scoped listing: GET /playlists returns ONLY the caller's own
	// playlists — including for admins (admin bypass applies to detail/mutations,
	// not to the list view).
	cu, _ := currentUser(r)
	if s.deps.PlaylistOwner != nil {
		rows, err := s.deps.PlaylistOwner.ListSyncedPlaylistsCountForOwner(
			r.Context(), sql.NullString{String: cu.ID, Valid: cu.ID != ""})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list synced playlists"})
			return
		}
		list := make([]core.SyncedPlaylist, 0, len(rows))
		for _, row := range rows {
			list = append(list, core.SyncedPlaylist{
				ID: row.ID, Source: row.Source, ExternalID: row.ExternalID,
				Name: row.Name, CoverURL: row.CoverUrl, Mode: row.Mode,
				SyncEnabled: row.SyncEnabled != 0, SyncIntervalSec: int(row.SyncIntervalSec),
				AutoDownload: row.AutoDownload != 0, LastSyncedAt: row.LastSyncedAt,
				TrackCount: int(row.TrackCount),
			})
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	// PlaylistOwner is always expected to be wired in production. When it is nil
	// (wiring slip or test misconfiguration), return an empty list rather than
	// bypassing ownership scoping and exposing all playlists.
	writeJSON(w, http.StatusOK, []core.SyncedPlaylist{})
}

func (s *Server) handleSyncedPlaylistDetail(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
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
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
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
	// This endpoint's sole purpose is triggering downloads: require auto_approve.
	// Check capability before ownership so the gate is visible regardless of
	// whether the playlist exists in the DB.
	if cu, ok := currentUser(r); !ok || !cu.Has(auth.CapAutoApprove) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "auto_approve capability required to trigger downloads"})
		return
	}
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
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
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
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
	id := chi.URLParam(r, "id")
	if !s.playlistAccessAllowed(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
		return
	}
	if err := svc.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	// FIX 4b: Best-effort remove any cover file for this playlist (ignore errors).
	if s.deps.DataDir != "" {
		coversDir := filepath.Join(s.deps.DataDir, "playlist-covers")
		for _, ext := range []string{"jpg", "png", "webp"} {
			_ = os.Remove(filepath.Join(coversDir, fmt.Sprintf("%s.%s", id, ext)))
		}
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
	CoverArtID string `json:"coverArtId"`
}

func (s *Server) handleAddSyncedTrack(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
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
		CoverArtID: body.CoverArtID,
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
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
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

// coverExtFromContentType maps an accepted image content-type to a file extension.
func coverExtFromContentType(ct string) (string, bool) {
	ct = strings.ToLower(strings.TrimSpace(ct))
	// Strip parameters (e.g. "image/jpeg; charset=utf-8").
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/jpeg":
		return "jpg", true
	case "image/png":
		return "png", true
	case "image/webp":
		return "webp", true
	}
	return "", false
}

const maxCoverBytes = 5 * 1024 * 1024 // 5 MB

// handleUploadPlaylistCover handles POST /api/v1/synced-playlists/{id}/cover.
// Accepts a multipart form with an "image" field; saves to dataDir/playlist-covers/{id}.<ext>.
func (s *Server) handleUploadPlaylistCover(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	id := chi.URLParam(r, "id")

	// Validate id format: must be alphanumeric + hyphens/underscores only.
	if !validPlaylistID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid playlist id"})
		return
	}
	if !s.playlistAccessAllowed(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
		return
	}

	// FIX 4a: limit total request body before parsing multipart.
	r.Body = http.MaxBytesReader(w, r.Body, maxCoverBytes+1<<20)

	if err := r.ParseMultipartForm(maxCoverBytes + 1*1024*1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image field is required"})
		return
	}
	defer file.Close()

	// Quick pre-filter: check the declared Content-Type before reading all bytes.
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = r.Header.Get("Content-Type")
	}
	if _, ok := coverExtFromContentType(ct); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported image type; use jpeg, png, or webp"})
		return
	}

	// Read up to maxCoverBytes+1 to detect oversized files without reading everything.
	limited := io.LimitReader(file, maxCoverBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read image"})
		return
	}
	if int64(len(data)) > maxCoverBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "image exceeds 5 MB limit"})
		return
	}

	// FIX 3: sniff actual content type from bytes; ignore client-declared type for the extension.
	sniffed := http.DetectContentType(data)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = strings.TrimSpace(sniffed[:i])
	}
	ext, ok := coverExtFromContentType(sniffed)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported image type; use jpeg, png, or webp"})
		return
	}

	// FIX 2: check playlist exists and is editable BEFORE writing to disk.
	det0, err := svc.Detail(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
		return
	}
	if det0.Mode != "once" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": playlistsync.ErrNotEditable.Error()})
		return
	}

	// Save to disk, overwriting any existing cover for this playlist.
	coversDir := filepath.Join(s.deps.DataDir, "playlist-covers")
	dst := filepath.Join(coversDir, fmt.Sprintf("%s.%s", id, ext))

	// Remove any existing cover files for this playlist (different extensions).
	for _, knownExt := range []string{"jpg", "png", "webp"} {
		if knownExt != ext {
			_ = os.Remove(filepath.Join(coversDir, fmt.Sprintf("%s.%s", id, knownExt)))
		}
	}

	if err := os.WriteFile(dst, data, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save image"})
		return
	}

	coverURL := fmt.Sprintf("/api/v1/playlists/%s/cover?v=%d", id, time.Now().Unix())
	det, err := svc.SetCover(r.Context(), id, coverURL)
	if err != nil {
		if errors.Is(err, playlistsync.ErrNotEditable) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

// handleServePlaylistCover handles GET /api/v1/synced-playlists/{id}/cover.
// Serves the uploaded cover image with a long cache TTL.
func (s *Server) handleServePlaylistCover(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Validate id format: must be alphanumeric + hyphens/underscores only.
	if !validPlaylistID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid playlist id"})
		return
	}

	coversDir := filepath.Join(s.deps.DataDir, "playlist-covers")

	var found string
	var foundExt string
	for _, ext := range []string{"jpg", "png", "webp"} {
		p := filepath.Join(coversDir, fmt.Sprintf("%s.%s", id, ext))
		if _, err := os.Stat(p); err == nil {
			found = p
			foundExt = ext
			break
		}
	}
	if found == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cover not found"})
		return
	}

	var ct string
	switch foundExt {
	case "jpg":
		ct = "image/jpeg"
	case "png":
		ct = "image/png"
	case "webp":
		ct = "image/webp"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	http.ServeFile(w, r, found)
}

// reorderBody is the PUT /synced-playlists/{id}/tracks/order request DTO.
type reorderBody struct {
	Order []core.TrackKey `json:"order"`
}

// handleReorderSyncedTracks handles PUT /api/v1/synced-playlists/{id}/tracks/order.
func (s *Server) handleReorderSyncedTracks(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
		return
	}
	var body reorderBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	det, err := svc.ReorderTracks(r.Context(), chi.URLParam(r, "id"), body.Order)
	if err != nil {
		if errors.Is(err, playlistsync.ErrNotEditable) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

// renameSyncedBody is the PUT /synced-playlists/{id} request DTO.
type renameSyncedBody struct {
	Name string `json:"name"`
}

// handleRenameSyncedPlaylist handles PUT /api/v1/synced-playlists/{id}.
// body: {"name":"..."} → 200 core.SyncedPlaylistDetail; 400 empty name; 404 not found.
func (s *Server) handleRenameSyncedPlaylist(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist sync unavailable"})
		return
	}
	if !s.playlistAccessAllowed(r, chi.URLParam(r, "id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
		return
	}
	var body renameSyncedBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	det, err := svc.Rename(r.Context(), chi.URLParam(r, "id"), body.Name)
	if err != nil {
		if errors.Is(err, playlistsync.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}
