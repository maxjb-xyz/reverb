package api

import "net/http"

func (s *Server) handleLibraryStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.LibraryStatus != nil {
		mode, state := s.deps.LibraryStatus()
		writeJSON(w, http.StatusOK, map[string]string{"mode": mode, "state": state})
		return
	}
	if s.library() != nil {
		writeJSON(w, http.StatusOK, map[string]string{"mode": "external", "state": "ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"mode": "external", "state": "unconfigured"})
}
