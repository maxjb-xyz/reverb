package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/auth"
)

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	caps := make([]string, 0)
	for _, c := range auth.AllCapabilities() {
		if cu.Caps[c.Key] {
			caps = append(caps, c.Key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           cu.ID,
		"username":     cu.Username,
		"roleName":     cu.RoleName,
		"isOwner":      cu.IsOwner,
		"capabilities": caps,
	})
}

func (s *Server) handleChangeUsername(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	var b struct {
		Username string `json:"username"`
	}
	if err := decode(r, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	b.Username = strings.TrimSpace(b.Username)
	if b.Username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username required"})
		return
	}
	if err := s.deps.Auth.ChangeOwnUsername(r.Context(), cu.ID, b.Username); err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken"})
		case errors.Is(err, auth.ErrUserNotFound):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update username"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": b.Username})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	var b struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decode(r, &b); err != nil || b.New == "" || b.Current == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current and new required"})
		return
	}
	if err := s.deps.Auth.ChangeOwnPassword(r.Context(), cu.ID, b.Current, b.New); err != nil {
		if writePasswordPolicyError(w, err) {
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current password incorrect"})
		return
	}
	// A password change invalidates every other session for this user (a stolen
	// session can no longer outlive a password reset). The caller's current
	// session is preserved so they stay signed in on this device.
	if err := s.deps.Auth.LogoutAll(r.Context(), cu.ID, s.tokenFromRequest(r)); err != nil {
		log.Printf("change-password: failed to revoke other sessions for user %s: %v", cu.ID, err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	if err := s.deps.Auth.LogoutAll(r.Context(), cu.ID, s.tokenFromRequest(r)); err != nil {
		log.Printf("logout-all: failed to delete sessions for user %s: %v", cu.ID, err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
