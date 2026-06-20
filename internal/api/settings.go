// internal/api/settings.go (stub — fully implemented in Task 6)
package api

import "net/http"

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"})
}
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "todo"})
}
