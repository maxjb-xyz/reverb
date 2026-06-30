package api

import (
	"net/http"

	"github.com/maxjb-xyz/reverb/internal/play"
)

// handlePlay serves POST /api/v1/plays.
// Decodes a play.PlayInput from the request body, records it for the session
// user via play.Service.Record, and responds 204 No Content on success.
func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	if s.deps.Play == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "play service unavailable"})
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var in play.PlayInput
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := s.deps.Play.Record(r.Context(), cu.ID, in); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
