package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
)

// handleStream proxies an audio stream from the library adapter, forwarding the
// inbound Range header upstream and copying back the status, Content-Type,
// Content-Length, Accept-Ranges, and Content-Range. Subsonic credentials never
// reach the browser.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	handle, err := lib.Stream(r.Context(), id, core.StreamOpts{}, r.Header.Get("Range"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer handle.Body.Close()

	h := w.Header()
	if handle.ContentType != "" {
		h.Set("Content-Type", handle.ContentType)
	}
	if handle.AcceptRanges != "" {
		h.Set("Accept-Ranges", handle.AcceptRanges)
	}
	if handle.ContentRange != "" {
		h.Set("Content-Range", handle.ContentRange)
	}
	if handle.ContentLength > 0 {
		h.Set("Content-Length", strconv.FormatInt(handle.ContentLength, 10))
	}
	status := handle.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, handle.Body)
}

// handleCover proxies cover art from the library adapter.
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	cover, err := lib.CoverArt(r.Context(), id, size)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer cover.Body.Close()
	if cover.ContentType != "" {
		w.Header().Set("Content-Type", cover.ContentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, cover.Body)
}
