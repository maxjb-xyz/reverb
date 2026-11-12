package api

import "net/http"

var errAnalysisUnavailable = map[string]string{"error": "audio analysis unavailable"}

// handleAnalysisStatus serves GET /api/v1/engine/analysis (managers): whether
// an audio analyzer is set up and how much of the library it has heard.
func (s *Server) handleAnalysisStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Analysis == nil {
		writeJSON(w, http.StatusServiceUnavailable, errAnalysisUnavailable)
		return
	}
	st, err := s.deps.Analysis.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleAnalysisRun serves POST /api/v1/engine/analysis/run (managers): carry
// on analyzing now instead of at the next tick. Responds 202.
func (s *Server) handleAnalysisRun(w http.ResponseWriter, r *http.Request) {
	if s.deps.Analysis == nil {
		writeJSON(w, http.StatusServiceUnavailable, errAnalysisUnavailable)
		return
	}
	s.deps.Analysis.Kick()
	w.WriteHeader(http.StatusAccepted)
}
