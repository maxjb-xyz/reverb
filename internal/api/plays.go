package api

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/scrobble"
)

// handlePlay serves POST /api/v1/plays.
// Decodes a play.PlayInput from the request body, records it for the session
// user via play.Service.RecordPlay, and responds 200 with {"id": playID}. The
// player sends the play once it qualifies (half the song or four minutes), then
// reports the final listened time with PATCH /plays/{id}.
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

	id, err := s.deps.Play.RecordPlay(r.Context(), cu.ID, in)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	// Enqueue for scrobbling if the scrobble service is wired in.
	// Resolve played_at: use the body value when set; fall back to now.
	if s.deps.Scrobble != nil {
		playedAt := in.PlayedAt
		if playedAt == 0 {
			playedAt = time.Now().Unix()
		}
		if err := s.deps.Scrobble.Enqueue(r.Context(), cu.ID, scrobble.ScrobblePlay{
			Track: scrobble.Track{
				Title:      in.Title,
				Artist:     in.Artist,
				Album:      in.Album,
				DurationMs: in.DurationMs,
			},
			PlayedAt: playedAt,
		}); err != nil {
			log.Printf("scrobble: enqueue after play user=%s: %v", cu.ID, err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// playProgressInput is the PATCH /plays/{id} body: the final listened time and
// whether the song was heard to the end. PascalCase like play.PlayInput.
type playProgressInput struct {
	MsPlayed  int
	Completed bool
}

// handleUpdatePlay serves PATCH /api/v1/plays/{id}.
// Records how much of a play was finally heard. Owner-scoped exactly like
// handleDeletePlay: the user id comes from the session, a non-owned or unknown
// id is a no-op, and the answer is always 204 so nothing leaks about the id.
// Values only ever rise, so a repeated or out-of-order update is harmless.
func (s *Server) handleUpdatePlay(w http.ResponseWriter, r *http.Request) {
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
	var in playProgressInput
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := s.deps.Play.UpdateProgress(r.Context(), cu.ID, id, in.MsPlayed, in.Completed); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
