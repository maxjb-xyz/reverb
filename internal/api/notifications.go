package api

import (
	"net/http"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// handleListNotifications serves GET /api/v1/notifications.
// Returns the caller's recent notifications (up to 50) and unread count.
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	if s.deps.Notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications unavailable"})
		return
	}
	cu, _ := currentUser(r)
	ctx := r.Context()

	items, err := s.deps.Notifications.ListForUser(ctx, cu.ID, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []core.Notification{}
	}
	unread, err := s.deps.Notifications.CountUnread(ctx, cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": items,
		"unread":        unread,
	})
}

// handleMarkNotificationsRead serves POST /api/v1/notifications/read.
// Body: { "ids"?: []string }. Empty/omitted ids → MarkAllRead.
// Responds with { "unread": int }.
func (s *Server) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if s.deps.Notifications == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "notifications unavailable"})
		return
	}
	cu, _ := currentUser(r)
	ctx := r.Context()

	var body struct {
		IDs []string `json:"ids"`
	}
	// Ignore decode errors — an empty/missing body is valid and means MarkAllRead.
	_ = decode(r, &body)

	if len(body.IDs) == 0 {
		if err := s.deps.Notifications.MarkAllRead(ctx, cu.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		if err := s.deps.Notifications.MarkRead(ctx, cu.ID, body.IDs); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	unread, err := s.deps.Notifications.CountUnread(ctx, cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": unread})
}
