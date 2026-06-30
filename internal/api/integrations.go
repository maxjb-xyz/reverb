package api

import (
	"net/http"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

const (
	keyLastfmAPIKey    = "scrobble:lastfm:api_key"
	keyLastfmAPISecret = "scrobble:lastfm:api_secret"
)

// handleGetLastfmIntegration serves GET /api/v1/admin/integrations/lastfm.
// Returns {apiKey, apiSecretSet} — the secret value is NEVER returned.
func (s *Server) handleGetLastfmIntegration(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	apiKey, _ := s.deps.Adapters.GetSetting(r.Context(), keyLastfmAPIKey)
	apiSecret, _ := s.deps.Adapters.GetSetting(r.Context(), keyLastfmAPISecret)

	writeJSON(w, http.StatusOK, map[string]any{
		"apiKey":       apiKey,
		"apiSecretSet": apiSecret != "",
	})
}

// putLastfmBody is the expected request body for PUT /admin/integrations/lastfm.
type putLastfmBody struct {
	APIKey    string `json:"apiKey"`
	APISecret string `json:"apiSecret"`
}

// handlePutLastfmIntegration serves PUT /api/v1/admin/integrations/lastfm.
// Stores apiKey unconditionally; only writes apiSecret when it is a real,
// non-empty, non-sentinel string (blank or sentinel = preserve stored value).
// Returns 204 on success.
func (s *Server) handlePutLastfmIntegration(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	var body putLastfmBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	// Always persist the API key (including empty string to clear it).
	if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{
		Key:   keyLastfmAPIKey,
		Value: body.APIKey,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save api key"})
		return
	}

	// Only overwrite the secret when the caller supplied a real, non-sentinel value.
	if body.APISecret != "" && body.APISecret != secretSentinel {
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{
			Key:   keyLastfmAPISecret,
			Value: body.APISecret,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save api secret"})
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
