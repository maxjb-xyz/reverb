package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/auth"
)

// handleRegistrationStatus is a public endpoint — GET /auth/registration-status.
// It returns {signupEnabled, invitesEnabled} so the frontend can decide whether
// to show the "Create account" link / signup page without requiring a session.
func (s *Server) handleRegistrationStatus(w http.ResponseWriter, r *http.Request) {
	pol, err := s.deps.Auth.GetRegPolicy(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load policy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"signupEnabled":  pol.SignupEnabled,
		"invitesEnabled": pol.InvitesEnabled,
	})
}

// handleSignup is a public endpoint — POST /auth/signup.
// It respects the registration policy: open signup or invite-based only.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Invite   string `json:"invite"`
	}
	if err := decode(r, &body); err != nil || body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}
	uid, err := s.deps.Auth.Signup(r.Context(), body.Username, body.Password, body.Invite)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSignupDisabled), errors.Is(err, auth.ErrInviteInvalid):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, auth.ErrUsernameTaken):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
		return
	}
	s.issueSession(w, r, uid)
}

// handleGetRegistration returns the current registration policy. Admin only.
func (s *Server) handleGetRegistration(w http.ResponseWriter, r *http.Request) {
	pol, err := s.deps.Auth.GetRegPolicy(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load policy"})
		return
	}
	writeJSON(w, http.StatusOK, pol)
}

// handlePatchRegistration updates the registration policy. Admin only.
func (s *Server) handlePatchRegistration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SignupEnabled  bool   `json:"signupEnabled"`
		InvitesEnabled bool   `json:"invitesEnabled"`
		DefaultRoleID  string `json:"defaultRoleId"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	pol := auth.RegPolicy{
		SignupEnabled:  body.SignupEnabled,
		InvitesEnabled: body.InvitesEnabled,
		DefaultRoleID:  body.DefaultRoleID,
	}
	if err := s.deps.Auth.SetRegPolicy(r.Context(), pol); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save policy"})
		return
	}
	writeJSON(w, http.StatusOK, pol)
}

// handleListInvites returns all invite rows. Admin only.
func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Auth.ListInvites(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list invites"})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateInvite creates and returns a new invite. Admin only.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RoleID    *string `json:"roleId"`
		ExpiresAt *int64  `json:"expiresAt"`
	}
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	cu, _ := currentUser(r)
	id, code, err := s.deps.Auth.CreateInvite(r.Context(), body.RoleID, body.ExpiresAt, cu.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create invite"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "code": code})
}

// handleDeleteInvite removes an invite by ID. Admin only.
func (s *Server) handleDeleteInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.deps.Auth.DeleteInviteByID(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not delete invite"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
