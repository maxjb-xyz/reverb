package api

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/request"
)

// errNoDownloader is returned by createOneRequest when auto_approve is set but
// no download manager is configured.
var errNoDownloader = errors.New("no downloader configured")

// createOneRequest runs the single-item request create/approve/pending logic.
// Returns (req, created, err) where created is false when the request already
// existed (dedup hit) and true when newly created (auto-approved+enqueued OR pending).
func (s *Server) createOneRequest(ctx context.Context, cu auth.CurrentUser, item core.RequestItem) (core.Request, bool, error) {
	req, existed, err := s.deps.Requests.Create(ctx, cu.ID, item)
	if err != nil {
		return core.Request{}, false, err
	}

	if existed {
		return req, false, nil
	}

	if cu.Has("auto_approve") {
		dl := s.downloads()
		if dl == nil {
			return core.Request{}, false, errNoDownloader
		}
		job, err := dl.Enqueue(ctx, downloadReqFromItem(item, cu.ID))
		if err != nil {
			return core.Request{}, false, err
		}
		req, err = s.deps.Requests.MarkApproved(ctx, req.ID, cu.ID, job.ID)
		if err != nil {
			return core.Request{}, false, err
		}
	} else {
		s.deps.Requests.NotifyPending(ctx, req)
	}

	return req, true, nil
}

// handleCreateRequest POST /requests
func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	var item core.RequestItem
	if err := decode(r, &item); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	cu, _ := currentUser(r)
	req, _, err := s.createOneRequest(r.Context(), cu, item)
	if err != nil {
		if errors.Is(err, errNoDownloader) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, req)
}

// handleBatchCreateRequests POST /requests/batch
func (s *Server) handleBatchCreateRequests(w http.ResponseWriter, r *http.Request) {
	// TODO(quota): enforce per-user request quota here (feature 2 — request quotas)

	var body struct {
		Items []core.RequestItem `json:"items"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	cu, _ := currentUser(r)
	ctx := r.Context()

	var (
		created  int
		skipped  int
		failed   int
		requests []core.Request
	)

	for _, item := range body.Items {
		req, wasCreated, err := s.createOneRequest(ctx, cu, item)
		if err != nil {
			log.Printf("batch request: item %q error: %v", item.ExternalID, err)
			failed++
			continue
		}
		requests = append(requests, req)
		if wasCreated {
			created++
		} else {
			skipped++
		}
	}

	_ = failed // logged above; not exposed in response per spec

	if requests == nil {
		requests = []core.Request{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"created":  created,
		"skipped":  skipped,
		"requests": requests,
	})
}

// handleListMyRequests GET /requests/mine
func (s *Server) handleListMyRequests(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	reqs, err := s.deps.Requests.ListForOwner(r.Context(), cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// handleListRequests GET /requests?status=
func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	reqs, err := s.deps.Requests.ListAll(r.Context(), statusFilter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// handleApproveRequest POST /requests/{id}/approve
func (s *Server) handleApproveRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cu, _ := currentUser(r)

	req, err := s.deps.Requests.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, request.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Guard: reject non-pending requests before enqueuing a download job.
	if req.Status != core.RequestPending {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "request is not pending"})
		return
	}

	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	job, err := dl.Enqueue(r.Context(), downloadReqFromRequest(req))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	approved, err := s.deps.Requests.MarkApproved(r.Context(), id, cu.ID, job.ID)
	if err != nil {
		if errors.Is(err, request.ErrNotPending) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "request is not pending"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, approved)
}

// handleDenyRequest POST /requests/{id}/deny
func (s *Server) handleDenyRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cu, _ := currentUser(r)

	var body struct {
		Reason string `json:"reason"`
	}
	// Ignore decode errors — reason is optional.
	_ = decode(r, &body)

	denied, err := s.deps.Requests.Deny(r.Context(), id, cu.ID, body.Reason)
	if err != nil {
		if errors.Is(err, request.ErrNotPending) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "request is not pending"})
			return
		}
		if errors.Is(err, request.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, denied)
}

// handleCancelRequest POST /requests/{id}/cancel
func (s *Server) handleCancelRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cu, _ := currentUser(r)

	err := s.deps.Requests.Cancel(r.Context(), id, cu.ID)
	if err != nil {
		if errors.Is(err, request.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if errors.Is(err, request.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

// granularityForKind maps a kind string to a DownloadGranularity.
// An empty kind defaults to GranularityTrack.
func granularityForKind(kind string) core.DownloadGranularity {
	if kind == "album" {
		return core.GranularityAlbum
	}
	return core.GranularityTrack
}

// downloadReqFromItem builds a DownloadRequest from a RequestItem.
// InitiatedBy is set to the requester (the person who created the request).
func downloadReqFromItem(item core.RequestItem, requesterID string) core.DownloadRequest {
	return core.DownloadRequest{
		Source:      item.Source,
		ExternalID:  item.ExternalID,
		Title:       item.Title,
		Artist:      item.Artist,
		Album:       item.Album,
		ISRC:        item.ISRC,
		DurationMs:  item.DurationMs,
		InitiatedBy: requesterID,
		Granularity: granularityForKind(item.Kind),
	}
}

// downloadReqFromRequest builds a DownloadRequest from a Request.
// InitiatedBy is set to the original requester (not the approver).
func downloadReqFromRequest(req core.Request) core.DownloadRequest {
	return core.DownloadRequest{
		Source:      req.Source,
		ExternalID:  req.ExternalID,
		Title:       req.Title,
		Artist:      req.Artist,
		Album:       req.Album,
		ISRC:        req.ISRC,
		DurationMs:  req.DurationMs,
		InitiatedBy: req.RequestedBy,
		Granularity: granularityForKind(req.Kind),
	}
}
