package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/maxjb-xyz/reverb/internal/discover"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

var errDiscoverUnavailable = map[string]string{"error": "discover unavailable"}

func (s *Server) discoverUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.deps.Discover == nil {
		writeJSON(w, http.StatusServiceUnavailable, errDiscoverUnavailable)
		return "", false
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return "", false
	}
	return cu.ID, true
}

// handleDiscover serves GET /api/v1/discover: the session user's Discover
// Weekly, with download progress for the new songs Reverb is fetching.
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.discoverUser(w, r)
	if !ok {
		return
	}
	wk, err := s.deps.Discover.Current(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wk)
}

// handleGetDiscoverPrefs serves GET /api/v1/discover/prefs.
func (s *Server) handleGetDiscoverPrefs(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.discoverUser(w, r)
	if !ok {
		return
	}
	p, err := s.deps.Discover.Prefs(r.Context(), uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePutDiscoverPrefs serves PUT /api/v1/discover/prefs: on/off, size,
// share of new songs and the keep rule.
func (s *Server) handlePutDiscoverPrefs(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.discoverUser(w, r)
	if !ok {
		return
	}
	var in discover.Prefs
	if err := decode(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	p, err := s.deps.Discover.SetPrefs(r.Context(), uid, in)
	if errors.Is(err, discover.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "size must be 10–100, newShare 0–100, keepRule playlist, played or always"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type discoverSettings struct {
	Downloads bool `json:"downloads"`
	WeeklyCap int  `json:"weeklyCap"`
}

// handleGetDiscoverSettings serves GET /api/v1/discover/settings (managers):
// whether Discover downloads songs ahead of time, and the per-user weekly cap.
func (s *Server) handleGetDiscoverSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Discover == nil {
		writeJSON(w, http.StatusServiceUnavailable, errDiscoverUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, discoverSettings{Downloads: s.deps.Discover.DownloadsEnabled(r.Context()), WeeklyCap: s.deps.Discover.WeeklyCap(r.Context())})
}

// handlePutDiscoverSettings serves PUT /api/v1/discover/settings (managers).
func (s *Server) handlePutDiscoverSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Discover == nil || s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, errDiscoverUnavailable)
		return
	}
	var in discoverSettings
	if err := decode(r, &in); err != nil || in.WeeklyCap < 0 || in.WeeklyCap > 500 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "weeklyCap must be 0–500"})
		return
	}
	dl := "true"
	if !in.Downloads {
		dl = "false"
	}
	for k, v := range map[string]string{discover.SettingDownloads: dl, discover.SettingWeeklyCap: strconv.Itoa(in.WeeklyCap)} {
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: k, Value: v}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	writeJSON(w, http.StatusOK, in)
}
