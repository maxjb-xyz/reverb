package api

import "net/http"

// embeddedSPA is replaced in Task 11 with a real embed.FS handler.
func (s *Server) embeddedSPA() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "crate", "note": "frontend not embedded yet"})
	})
}
