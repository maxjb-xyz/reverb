package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
)

// CoverageService is the slice of *coverage.Service the API needs. *coverage.Service
// satisfies it. Any may be nil when no DiscoSource-capable search adapter is enabled,
// in which case the handlers return 503.
type CoverageService interface {
	ArtistDetail(ctx context.Context, source, id string) (core.ArtistDetail, error)
	StreamCoverage(ctx context.Context, source, id string) <-chan core.AlbumCoverage
	AlbumDetail(ctx context.Context, source, id string) (core.AlbumDetail, error)
}

// coverage returns the currently active coverage service under the read lock.
// It may be nil when no coverage-capable source is configured.
func (s *Server) coverage() CoverageService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.coverage
}

func (s *Server) handleArtistDetail(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	if cov == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage unavailable"})
		return
	}
	det, err := cov.ArtistDetail(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleArtistCoverage(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	flusher, ok := w.(http.Flusher)
	if cov == nil || !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage stream unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ch := cov.StreamCoverage(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	for {
		select {
		case <-r.Context().Done():
			return
		case c, open := <-ch:
			if !open {
				return
			}
			b, err := json.Marshal(c)
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

func (s *Server) handleAlbumDetail(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	if cov == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage unavailable"})
		return
	}
	det, err := cov.AlbumDetail(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleLibraryPlaylist(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	pl, err := lib.GetPlaylist(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pl)
}

// batchDownloadBody is the POST /downloads/batch request DTO. Each ref enqueues
// one download; per-item failures (e.g. dedup) are skipped without aborting the batch.
type batchDownloadBody struct {
	Tracks []core.ExternalTrackRef `json:"tracks"`
}

func (s *Server) handleBatchDownload(w http.ResponseWriter, r *http.Request) {
	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	var body batchDownloadBody
	if err := decode(r, &body); err != nil || len(body.Tracks) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tracks is required"})
		return
	}
	jobs := []core.DownloadJob{}
	for _, t := range body.Tracks {
		job, err := dl.Enqueue(r.Context(), core.DownloadRequest{
			Source: t.Source, ExternalID: t.ExternalID, Artist: t.Artist,
			Title: t.Title, Album: t.Album, ISRC: t.ISRC, DurationMs: t.DurationMs,
		})
		if err != nil {
			continue // dedup-join / per-item failure shouldn't abort the batch
		}
		jobs = append(jobs, job)
	}
	writeJSON(w, http.StatusOK, jobs)
}
