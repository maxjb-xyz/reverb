package api

import "net/http"

// handlePendingRestart reports whether any adapter/settings change has been made
// since startup. With restart-to-apply (M4a) the UI shows a banner when true.
func (s *Server) handlePendingRestart(w http.ResponseWriter, r *http.Request) {
	dirty := false
	if s.deps.ConfigDirty != nil {
		dirty = s.deps.ConfigDirty.Dirty()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"pendingRestart": dirty})
}
