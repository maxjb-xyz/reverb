package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type createPlaylistBody struct {
	Name string `json:"name"`
}

type renamePlaylistBody struct {
	Name string `json:"name"`
}

type removeTracksBody struct {
	Indices []int `json:"indices"`
}

// handleCreatePlaylist creates a new blank managed playlist (source="local", mode="once").
// POST /api/v1/library/playlists  body {"name":"..."} → 201 core.SyncedPlaylistDetail
func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	svc := s.sync()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "playlist service unavailable"})
		return
	}
	var body createPlaylistBody
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	det, err := svc.CreateManaged(r.Context(), strings.TrimSpace(body.Name))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, det)
}

// handleRenamePlaylist renames an existing library playlist.
// PUT /api/v1/library/playlist/{id}  body {"name":"..."} → {"ok":true}
func (s *Server) handleRenamePlaylist(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	var body renamePlaylistBody
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if err := lib.RenamePlaylist(r.Context(), chi.URLParam(r, "id"), strings.TrimSpace(body.Name)); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDeletePlaylist deletes an existing library playlist.
// DELETE /api/v1/library/playlist/{id} → {"ok":true}
func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	if err := lib.DeletePlaylist(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRemovePlaylistTracks removes tracks at the given zero-based indices from a playlist.
// POST /api/v1/library/playlist/{id}/remove  body {"indices":[2]} → {"ok":true}
func (s *Server) handleRemovePlaylistTracks(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	var body removeTracksBody
	if err := decode(r, &body); err != nil || len(body.Indices) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "indices is required"})
		return
	}
	if err := lib.RemovePlaylistTracks(r.Context(), chi.URLParam(r, "id"), body.Indices); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
