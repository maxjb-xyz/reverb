package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/engine"

	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

var errIndexUnavailable = map[string]string{"error": "recommendation index unavailable"}

// handleIndexStatus serves GET /api/v1/engine/index (managers): how much of
// the library and the artist-similarity map the recommendation index covers.
func (s *Server) handleIndexStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.LibIndex == nil {
		writeJSON(w, http.StatusServiceUnavailable, errIndexUnavailable)
		return
	}
	st, err := s.deps.LibIndex.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleIndexRebuild serves POST /api/v1/engine/index/rebuild (managers):
// queue a re-index now. Responds 202; progress shows in GET /engine/index.
func (s *Server) handleIndexRebuild(w http.ResponseWriter, r *http.Request) {
	if s.deps.LibIndex == nil {
		writeJSON(w, http.StatusServiceUnavailable, errIndexUnavailable)
		return
	}
	s.deps.LibIndex.Kick()
	w.WriteHeader(http.StatusAccepted)
}

type similarArtistsResponse struct {
	Artist  string                   `json:"artist"`
	Similar []libindex.SimilarArtist `json:"similar"`
}

// handleSimilarArtists serves GET /api/v1/artists/similar?name=<artist>: the
// stored "fans also like" list for an artist, best first, each marked with
// how many of its tracks the library owns.
func (s *Server) handleSimilarArtists(w http.ResponseWriter, r *http.Request) {
	if s.deps.LibIndex == nil {
		writeJSON(w, http.StatusServiceUnavailable, errIndexUnavailable)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	sim, err := s.deps.LibIndex.Similar(r.Context(), matching.ArtistKey(name))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, similarArtistsResponse{Artist: name, Similar: sim})
}

var errEngineUnavailable = map[string]string{"error": "recommendation engine unavailable"}

type engineNextRequest struct {
	Seed            engine.Seed `json:"seed"`
	Lane            string      `json:"lane"`
	FromYear        int         `json:"fromYear"`
	ToYear          int         `json:"toYear"`
	Exclude         []string    `json:"exclude"`
	IncludeExternal bool        `json:"includeExternal"`
	N               int         `json:"n"`
}

// handleEngineNext serves POST /api/v1/engine/next: the next songs for a radio
// station (seeded by a track, artist or genre, or the user's own taste) or a
// DJ lane, each with a structured reason. Radio refills pass the queue so far
// in exclude.
func (s *Server) handleEngineNext(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var in engineNextRequest
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(in.Exclude) > 2000 {
		in.Exclude = in.Exclude[len(in.Exclude)-2000:]
	}
	res, err := s.deps.Engine.Next(r.Context(), cu.ID, engine.Request{
		Seed: in.Seed, Lane: in.Lane, FromYear: in.FromYear, ToYear: in.ToYear,
		Exclude: in.Exclude, IncludeExternal: in.IncludeExternal, N: in.N,
	})
	switch {
	case errors.Is(err, engine.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid station"})
	case errors.Is(err, engine.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "seed not in the library index yet"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

// handleEngineStations serves GET /api/v1/engine/stations?n=: radio stations
// suggested from the session user's taste (favourite artists, their genres).
func (s *Server) handleEngineStations(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 24 {
		n = 8
	}
	sug, err := s.deps.Engine.Suggestions(r.Context(), cu.ID, n)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stations": sug})
}

// handleEngineGenres serves GET /api/v1/engine/genres: the library's genres,
// biggest first, for picking a genre station or mix seed.
func (s *Server) handleEngineGenres(w http.ResponseWriter, r *http.Request) {
	if s.deps.Engine == nil {
		writeJSON(w, http.StatusServiceUnavailable, errEngineUnavailable)
		return
	}
	gs, err := s.deps.Engine.Genres(r.Context(), 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"genres": gs})
}
