package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/maximusjb/crate/internal/core"
)

// handleEverywhere streams per-source search envelopes as Server-Sent Events.
// Each event is `data: <Envelope JSON>\n\n`, flushed immediately. Each result in
// an envelope is already pre-matched by the MatchingService (via the aggregator).
// The handler returns when the aggregator closes its channel or the client
// disconnects (r.Context().Done()).
func (s *Server) handleEverywhere(w http.ResponseWriter, r *http.Request) {
	if s.deps.SearchAggregator == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no search sources configured"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q is required"})
		return
	}
	var et core.EntityType
	switch r.URL.Query().Get("type") {
	case "album":
		et = core.EntityAlbum
	case "artist":
		et = core.EntityArtist
	default:
		et = core.EntityTrack
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.deps.SearchAggregator.Stream(r.Context(), q, et)
	for {
		select {
		case <-r.Context().Done():
			return
		case env, open := <-ch:
			if !open {
				return
			}
			b, err := json.Marshal(env)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
