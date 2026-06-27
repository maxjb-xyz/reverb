package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/auth"
)

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.deps.Auth.ListRoles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list roles"})
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
	}
	if err := decode(r, &b); err != nil || b.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	id, err := s.deps.Auth.CreateRole(r.Context(), b.Name, b.Capabilities)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCapability):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown capability"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create role"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var b struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
	}
	if err := decode(r, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := s.deps.Auth.UpdateRole(r.Context(), id, b.Name, b.Capabilities); err != nil {
		switch {
		case errors.Is(err, auth.ErrSystemRole):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "system role is protected"})
		case errors.Is(err, auth.ErrInvalidCapability):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown capability"})
		case errors.Is(err, auth.ErrRoleNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "role not found"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update role"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.deps.Auth.DeleteRole(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, auth.ErrSystemRole):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "system role is protected"})
		case errors.Is(err, auth.ErrRoleInUse):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "role is assigned to users"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not delete role"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, auth.AllCapabilities())
}
