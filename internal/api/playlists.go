package api

import (
	"net/http"
	"strings"
)

type createPlaylistBody struct {
	Name string `json:"name"`
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
