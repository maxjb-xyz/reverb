package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
)

// createDownloadBody is the POST /downloads request DTO.
type createDownloadBody struct {
	Source        string `json:"source"`
	ExternalID    string `json:"externalId"`
	Artist        string `json:"artist"`
	Title         string `json:"title"`
	Album         string `json:"album"`
	ISRC          string `json:"isrc"`
	DurationMs    int    `json:"durationMs"`
	Downloader    string `json:"downloader"`
	PlayWhenReady bool   `json:"playWhenReady"`
}

func (s *Server) handleCreateDownload(w http.ResponseWriter, r *http.Request) {
	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	var body createDownloadBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.ExternalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "externalId is required"})
		return
	}
	job, err := dl.Enqueue(r.Context(), core.DownloadRequest{
		Source:        body.Source,
		ExternalID:    body.ExternalID,
		Artist:        body.Artist,
		Title:         body.Title,
		Album:         body.Album,
		ISRC:          body.ISRC,
		DurationMs:    body.DurationMs,
		Downloader:    body.Downloader,
		PlayWhenReady: body.PlayWhenReady,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusOK, []core.DownloadJob{})
		return
	}
	jobs, err := dl.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list downloads"})
		return
	}
	if jobs == nil {
		jobs = []core.DownloadJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := dl.Cancel(r.Context(), id); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// retryBody is the OPTIONAL JSON body for POST /downloads/{id}/retry.
// An absent or empty body is tolerated (plain retry, no manual URL).
type retryBody struct {
	ManualURL string `json:"manualUrl"`
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	id := chi.URLParam(r, "id")

	// Decode an optional JSON body. Tolerate: no body, empty body, EOF — all
	// resolve to manualURL="" (plain retry). Only a well-formed JSON object with
	// "manualUrl" is acted on.
	var manualURL string
	if r.Body != nil {
		raw, rerr := io.ReadAll(r.Body)
		if rerr == nil && len(raw) > 0 {
			var rb retryBody
			if jerr := json.Unmarshal(raw, &rb); jerr == nil {
				manualURL = rb.ManualURL
			}
		}
	}

	job, err := dl.Retry(r.Context(), id, manualURL)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}
