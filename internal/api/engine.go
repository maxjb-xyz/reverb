package api

import (
	"net/http"
	"strings"

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
