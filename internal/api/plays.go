package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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

// handleDeletePlay serves DELETE /api/v1/plays/{id}.
// Deletes a single play OWNED BY the session user. Owner-scoping is enforced in
// the query (WHERE id = ? AND user_id = ?): the user id is taken from the
// session and NEVER from the request — a user can never delete another user's
// play. A non-existent or non-owned id matches zero rows and still returns 204,
// so the response leaks no information about whether the play exists.
func (s *Server) handleDeletePlay(w http.ResponseWriter, r *http.Request) {
	if s.deps.Play == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "play service unavailable"})
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing play id"})
		return
	}

	if err := s.deps.Play.Delete(r.Context(), cu.ID, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
