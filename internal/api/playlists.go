package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type createPlaylistBody struct {
	Name string `json:"name"`
}

type addTracksBody struct {
	TrackIDs []string `json:"trackIds"`
}

// handleCreatePlaylist creates a new (empty) library playlist.
// POST /api/v1/library/playlists  body {"name":"..."} → 201 core.Playlist
func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	var body createPlaylistBody
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	pl, err := lib.CreatePlaylist(r.Context(), strings.TrimSpace(body.Name))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, pl)
}

// handleAddTracksToPlaylist appends tracks to an existing library playlist.
// POST /api/v1/library/playlists/{id}/tracks  body {"trackIds":["..."]} → {"ok":true}
func (s *Server) handleAddTracksToPlaylist(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	var body addTracksBody
	if err := decode(r, &body); err != nil || len(body.TrackIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trackIds is required"})
		return
	}
	if err := lib.AddTracksToPlaylist(r.Context(), chi.URLParam(r, "id"), body.TrackIDs); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
