package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/maximusjb/crate/internal/registry"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type passwordBody struct {
	Password string `json:"password"`
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	req, _ := s.deps.Auth.IsSetupRequired(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"setupRequired": req})
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	req, _ := s.deps.Auth.IsSetupRequired(r.Context())
	if !req {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "setup already complete"})
		return
	}
	var body passwordBody
	if err := decode(r, &body); err != nil || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password required"})
		return
	}
	if err := s.deps.Auth.SetAdminPassword(r.Context(), body.Password); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not set password"})
		return
	}
	s.issueSession(w, r.Context())
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body passwordBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	ok, _ := s.deps.Auth.CheckLogin(r.Context(), body.Password)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	s.issueSession(w, r.Context())
}

func (s *Server) issueSession(w http.ResponseWriter, ctx context.Context) {
	tok, err := s.deps.Auth.CreateSession(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	s.setSessionCookie(w, tok, s.deps.Dev)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": tok})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = s.deps.Auth.Logout(r.Context(), s.tokenFromRequest(r))
	s.setSessionCookie(w, "", s.deps.Dev)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

type adapterInfo struct {
	Type         string                `json:"type"`
	Name         string                `json:"name"`
	ConfigSchema registry.ConfigSchema `json:"configSchema"`
	Capabilities []string              `json:"capabilities"`
}

func (s *Server) handleAdaptersAvailable(w http.ResponseWriter, r *http.Request) {
	var out []adapterInfo
	for _, reg := range []*registry.Registry{s.deps.Library, s.deps.Search, s.deps.Downloader} {
		for _, name := range reg.Names() {
			p, err := reg.Create(name)
			if err != nil {
				continue
			}
			out = append(out, adapterInfo{
				Type:         p.Type(),
				Name:         p.Name(),
				ConfigSchema: p.ConfigSchema(),
				Capabilities: registry.DescribeCapabilities(p),
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
