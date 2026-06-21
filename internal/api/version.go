package api

import "net/http"

// handleVersion reports the build version. Empty Deps.Version (e.g. zero-value
// Deps in tests, or a build without -ldflags) reports "dev".
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	v := s.deps.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}
