package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/auth"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.deps.Auth.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list users"})
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
		RoleID   string `json:"roleId"`
	}
	if err := decode(r, &b); err != nil || b.Username == "" || b.Password == "" || b.RoleID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, password and roleId required"})
		return
	}
	id, err := s.deps.Auth.CreateUser(r.Context(), b.Username, b.Password, b.RoleID)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "username taken"})
		case errors.Is(err, auth.ErrRoleNotFound):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role not found"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create user"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		RoleID   *string `json:"roleId"`
		Disabled *bool   `json:"disabled"`
	}
	if err := decode(r, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if b.RoleID != nil {
		if err := s.deps.Auth.UpdateUserRole(r.Context(), id, *b.RoleID); err != nil {
			switch {
			case errors.Is(err, auth.ErrOwnerProtected):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "owner account is protected"})
			case errors.Is(err, auth.ErrLastAdmin):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "would leave no administrator"})
			case errors.Is(err, auth.ErrRoleNotFound):
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role not found"})
			default:
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update role"})
			}
			return
		}
	}
	if b.Disabled != nil {
		if err := s.deps.Auth.SetUserDisabled(r.Context(), id, *b.Disabled); err != nil {
			switch {
			case errors.Is(err, auth.ErrOwnerProtected):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "owner account is protected"})
			case errors.Is(err, auth.ErrLastAdmin):
				writeJSON(w, http.StatusConflict, map[string]string{"error": "would leave no administrator"})
			default:
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update user"})
			}
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.deps.Auth.DeleteUser(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, auth.ErrOwnerProtected):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "owner account is protected"})
		case errors.Is(err, auth.ErrLastAdmin):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "would leave no administrator"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not delete user"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Password string `json:"password"`
	}
	if err := decode(r, &b); err != nil || b.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password required"})
		return
	}
	if err := s.deps.Auth.AdminSetPassword(r.Context(), id, b.Password); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not reset password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
