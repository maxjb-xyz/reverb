//go:build !prod

package api

import "net/http"

// embeddedSPA (dev/test stub) — replaced by embed_prod.go under -tags prod.
func (s *Server) embeddedSPA() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "reverb", "note": "frontend not embedded yet"})
	})
}
