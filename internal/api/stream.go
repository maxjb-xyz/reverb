package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
)

// isCanonicalID reports whether id carries a catalog-entity prefix. Only these
// ids may be passed to the resolver; raw backend ids must never reach it.
func isCanonicalID(id string) bool {
	return strings.HasPrefix(id, "trk_") ||
		strings.HasPrefix(id, "alb_") ||
		strings.HasPrefix(id, "art_")
}

// handleStream proxies an audio stream from the library adapter, forwarding the
// inbound Range header upstream and copying back the status, Content-Type,
// Content-Length, Accept-Ranges, and Content-Range. Subsonic credentials never
// reach the browser.
//
// For canonical ids (trk_/alb_/art_) the resolver is consulted first to obtain
// the current backend track id; raw backend ids pass through directly.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if isCanonicalID(id) {
		if s.deps.Resolver == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		addr, err := s.deps.Resolver.Resolve(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if !addr.Found || addr.BackendID == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		id = addr.BackendID
	}
	s.serveStream(w, r, lib, id)
}

// handleCover proxies cover art from the library adapter.
//
// For canonical ids (trk_/alb_/art_) the resolver is consulted first to obtain
// the current backend cover art id; raw backend ids pass through directly.
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if isCanonicalID(id) {
		if s.deps.Resolver == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		addr, err := s.deps.Resolver.Resolve(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if !addr.Found || addr.CoverArtID == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		id = addr.CoverArtID
	}
	s.serveCover(w, r, lib, id, size)
}

// serveStream is the shared adapter-calling body for handleStream and its
// canonical-id resolution path. It threads the Range header and copies back
// all relevant response headers.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, lib library.LibraryAdapter, backendID string) {
	handle, err := lib.Stream(r.Context(), backendID, core.StreamOpts{}, r.Header.Get("Range"))
	if err != nil {
		if errors.Is(err, core.ErrLibraryItemNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
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

// serveCover is the shared adapter-calling body for handleCover and its
// canonical-id resolution path. Cache-Control is set after the adapter call
// (consistent with the original handler's placement).
func (s *Server) serveCover(w http.ResponseWriter, r *http.Request, lib library.LibraryAdapter, backendID string, size int) {
	cover, err := lib.CoverArt(r.Context(), backendID, size)
	if err != nil {
		if errors.Is(err, core.ErrLibraryItemNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
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
