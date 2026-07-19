package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/library/lyrics"
)

// LyricsProvider is the seam the lyrics handler needs; *lyrics.Service
// satisfies it. Nil in tests/legacy wiring — handler serves 204.
type LyricsProvider interface {
	Get(ctx context.Context, req lyrics.Request) (lyrics.Lyrics, bool, error)
}

func (s *Server) handleTrackLyrics(w http.ResponseWriter, r *http.Request) {
	if s.deps.Lyrics == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id := chi.URLParam(r, "id")
	q := r.URL.Query()
	durationMs, _ := strconv.Atoi(q.Get("durationMs"))
	req := lyrics.Request{
		TrackID: id,
		Query: lyrics.Query{
			Artist:     q.Get("artist"),
			Title:      q.Get("title"),
			Album:      q.Get("album"),
			DurationMs: durationMs,
		},
	}
	// Local file access is optional, mirroring the peaks handler.
	if paths, ok := s.library().(localTrackPath); ok {
		if p, ok := paths.LocalTrackPath(id); ok {
			req.LocalPath = p
		}
	}
	lyr, ok, err := s.deps.Lyrics.Get(r.Context(), req)
	if err != nil || !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if lyr.Synced {
		writeJSON(w, http.StatusOK, map[string]any{"synced": true, "lines": lyr.Lines})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"synced": false, "plain": lyr.Plain})
}
