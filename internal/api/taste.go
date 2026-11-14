package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

var errTasteUnavailable = map[string]string{"error": "taste service unavailable"}

// handleListen serves POST /api/v1/listens: one finished listen (completed,
// skipped early/late, or stopped) reported by the web player for the session
// user. Responds 204.
func (s *Server) handleListen(w http.ResponseWriter, r *http.Request) {
	if s.deps.Taste == nil {
		writeJSON(w, http.StatusServiceUnavailable, errTasteUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var in taste.ListenInput
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := s.deps.Taste.RecordListen(r.Context(), cu.ID, in); err != nil {
		writeTasteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tasteProfileResponse struct {
	TopArtists    []taste.ArtistTaste `json:"topArtists"`
	TopTracks     []taste.TrackTaste  `json:"topTracks"`
	TrackCount    int                 `json:"trackCount"`
	ArtistCount   int                 `json:"artistCount"`
	HiddenTracks  int                 `json:"hiddenTracks"`
	HiddenArtists int                 `json:"hiddenArtists"`
}

// handleTasteProfile serves GET /api/v1/taste/profile: the session user's top
// artists and tracks by taste score, for transparency ("why does Reverb think
// I like this?") and debugging.
func (s *Server) handleTasteProfile(w http.ResponseWriter, r *http.Request) {
	if s.deps.Taste == nil {
		writeJSON(w, http.StatusServiceUnavailable, errTasteUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	p, err := s.deps.Taste.Profile(r.Context(), cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tasteProfileResponse{
		TopArtists:    p.TopArtists(20),
		TopTracks:     p.TopTracks(20),
		TrackCount:    len(p.Tracks),
		ArtistCount:   len(p.Artists),
		HiddenTracks:  len(p.HiddenTracks),
		HiddenArtists: len(p.HiddenArtists),
	})
}

// handleListTasteFeedback serves GET /api/v1/taste/feedback: the session user's
// thumbs-downs, newest first.
func (s *Server) handleListTasteFeedback(w http.ResponseWriter, r *http.Request) {
	if s.deps.Taste == nil {
		writeJSON(w, http.StatusServiceUnavailable, errTasteUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	list, err := s.deps.Taste.ListFeedback(r.Context(), cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleSetTasteFeedback serves PUT /api/v1/taste/feedback: thumbs-down a
// track ("not this song") or an artist ("less of this artist").
func (s *Server) handleSetTasteFeedback(w http.ResponseWriter, r *http.Request) {
	if s.deps.Taste == nil {
		writeJSON(w, http.StatusServiceUnavailable, errTasteUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var in taste.FeedbackInput
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	f, err := s.deps.Taste.SetFeedback(r.Context(), cu.ID, in)
	if err != nil {
		writeTasteErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleClearTasteFeedback serves DELETE /api/v1/taste/feedback/{kind}/{key}.
// Owner-scoped by the session user; clearing a missing row is still 204.
func (s *Server) handleClearTasteFeedback(w http.ResponseWriter, r *http.Request) {
	if s.deps.Taste == nil {
		writeJSON(w, http.StatusServiceUnavailable, errTasteUnavailable)
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// Artist keys are normalized names and may arrive percent-encoded.
	key, err := url.PathUnescape(chi.URLParam(r, "key"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid key"})
		return
	}
	if err := s.deps.Taste.ClearFeedback(r.Context(), cu.ID, chi.URLParam(r, "kind"), key); err != nil {
		writeTasteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeTasteErr(w http.ResponseWriter, err error) {
	if errors.Is(err, taste.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

// recordTasteSignal records a playlist-add/download/request taste signal for
// userID. Best-effort: the user's action already succeeded, so a failure here
// is logged, never surfaced.
func (s *Server) recordTasteSignal(ctx context.Context, userID, kind string, ref taste.TrackRef) {
	if s.deps.Taste == nil || userID == "" {
		return
	}
	if err := s.deps.Taste.RecordSignal(ctx, userID, kind, ref); err != nil {
		log.Printf("taste: record %s signal for %s: %v", kind, userID, err)
	}
}

// tasteRefFor builds a taste.TrackRef from the source/externalId pairs the
// playlist, download and request APIs carry. A "library" source is a library
// track: identify it by metadata only, exactly like play.Record, so the signal
// lands on the same catalog entity as that track's listens.
func tasteRefFor(source, externalID, title, artist, album, isrc string, durationMs int) taste.TrackRef {
	ref := taste.TrackRef{Title: title, Artist: artist, Album: album, ISRC: isrc, DurationMs: durationMs}
	if source == "library" {
		ref.LibraryTrackID = externalID
	} else if title != "" {
		ref.Source, ref.ExternalID = source, externalID
	}
	return ref
}
