package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/mixes"
)

var errMixesUnavailable = map[string]string{"error": "mixes unavailable"}

type mixBody struct {
	Name   string       `json:"name"`
	Recipe mixes.Recipe `json:"recipe"`
}

func writeMixErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mixes.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mix"})
	case errors.Is(err, mixes.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "mix not found"})
	case errors.Is(err, mixes.ErrReadOnly):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "daily mixes are made by Reverb and can't be edited"})
	case errors.Is(err, mixes.ErrTooMany):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "too many mixes"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// mixUser checks the service and session; it writes the error and returns
// false when the request can't go on.
func (s *Server) mixUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.deps.Mixes == nil {
		writeJSON(w, http.StatusServiceUnavailable, errMixesUnavailable)
		return "", false
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	return cu.ID, true
}

// handleListMixes serves GET /api/v1/mixes: the session user's daily mixes
// (rebuilt each day from their taste) and custom mixes, with their songs.
func (s *Server) handleListMixes(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.mixUser(w, r)
	if !ok {
		return
	}
	list, err := s.deps.Mixes.List(r.Context(), uid)
	if err != nil {
		writeMixErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mixes": list})
}

// handleGetMix serves GET /api/v1/mixes/{id}.
func (s *Server) handleGetMix(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.mixUser(w, r)
	if !ok {
		return
	}
	m, err := s.deps.Mixes.Get(r.Context(), uid, chi.URLParam(r, "id"))
	if err != nil {
		writeMixErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleCreateMix serves POST /api/v1/mixes: save a custom mix and build it.
func (s *Server) handleCreateMix(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.mixUser(w, r)
	if !ok {
		return
	}
	var in mixBody
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	m, err := s.deps.Mixes.Create(r.Context(), uid, in.Name, in.Recipe)
	if err != nil {
		writeMixErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// handleUpdateMix serves PUT /api/v1/mixes/{id}: rename / change the recipe of
// a custom mix, rebuilding it.
func (s *Server) handleUpdateMix(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.mixUser(w, r)
	if !ok {
		return
	}
	var in mixBody
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	m, err := s.deps.Mixes.Update(r.Context(), uid, chi.URLParam(r, "id"), in.Name, in.Recipe)
	if err != nil {
		writeMixErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleRefreshMix serves POST /api/v1/mixes/{id}/refresh: new songs now.
func (s *Server) handleRefreshMix(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.mixUser(w, r)
	if !ok {
		return
	}
	m, err := s.deps.Mixes.Refresh(r.Context(), uid, chi.URLParam(r, "id"))
	if err != nil {
		writeMixErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleDeleteMix serves DELETE /api/v1/mixes/{id} (custom mixes only).
func (s *Server) handleDeleteMix(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.mixUser(w, r)
	if !ok {
		return
	}
	if err := s.deps.Mixes.Delete(r.Context(), uid, chi.URLParam(r, "id")); err != nil {
		writeMixErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
