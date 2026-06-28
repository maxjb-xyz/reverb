package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/request"
)

// handleCreateRequest POST /requests
func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	var item core.RequestItem
	if err := decode(r, &item); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	cu, _ := currentUser(r)
	req, existed, err := s.deps.Requests.Create(r.Context(), cu.ID, item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if existed {
		writeJSON(w, http.StatusOK, req)
		return
	}

	if cu.Has("auto_approve") {
		dl := s.downloads()
		if dl == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
			return
		}
		job, err := dl.Enqueue(r.Context(), downloadReqFromItem(item, cu.ID))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		req, err = s.deps.Requests.MarkApproved(r.Context(), req.ID, cu.ID, job.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		s.deps.Requests.NotifyPending(r.Context(), req)
	}

	writeJSON(w, http.StatusOK, req)
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
