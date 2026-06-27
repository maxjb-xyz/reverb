package api

import (
	"encoding/json"
	"net/http"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONPending wraps a payload with the restart-to-apply flag so the client can
// surface the "Restart to apply" banner immediately after a mutation.
func writeJSONPending(w http.ResponseWriter, status int, v any, pending bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Data           any  `json:"data"`
		PendingRestart bool `json:"pendingRestart"`
	}{Data: v, PendingRestart: pending})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type credsBody struct {
	Username string `json:"username"`
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
	if req, _ := s.deps.Auth.IsSetupRequired(r.Context()); !req {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "setup already complete"})
		return
	}
	var b credsBody
	if err := decode(r, &b); err != nil || b.Username == "" || b.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}
	uid, err := s.deps.Auth.SetupOwner(r.Context(), b.Username, b.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create owner"})
		return
	}
	s.issueSession(w, r, uid)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var b credsBody
	if err := decode(r, &b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	uid, err := s.deps.Auth.Login(r.Context(), b.Username, b.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	s.issueSession(w, r, uid)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID string) {
	tok, err := s.deps.Auth.CreateSession(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	s.setSessionCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": userID})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = s.deps.Auth.Logout(r.Context(), s.tokenFromRequest(r))
	s.setSessionCookie(w, r, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	cu, _ := currentUser(r)
	caps := make([]string, 0, len(cu.Caps))
	for _, c := range auth.AllCapabilities() {
		if cu.Caps[c.Key] {
			caps = append(caps, c.Key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": cu.ID, "username": cu.Username, "roleId": cu.RoleID,
		"roleName": cu.RoleName, "isOwner": cu.IsOwner, "capabilities": caps,
	})
}

type adapterInfo struct {
	Type         string                `json:"type"`
	Name         string                `json:"name"`
	ConfigSchema registry.ConfigSchema `json:"configSchema"`
	Capabilities []string              `json:"capabilities"`
}

func (s *Server) handleAdaptersAvailable(w http.ResponseWriter, r *http.Request) {
	out := make([]adapterInfo, 0)
	for _, reg := range []*registry.Registry{s.deps.Lib, s.deps.Search, s.deps.Downloader} {
		if reg == nil {
			continue
		}
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
