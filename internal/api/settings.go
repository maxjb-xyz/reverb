package api

import (
	"net/http"
	"regexp"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

const (
	keyAccentColor        = "accent_color"
	keyDynamicBackground  = "dynamic_background"
	keyDefaultDownloader  = "default_downloader"
	keyLibraryBackendMode = "library_backend_mode"
	defaultAccentColor    = "#F0354B"
)

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type settingsDTO struct {
	AccentColor        string `json:"accentColor"`
	DynamicBackground  bool   `json:"dynamicBackground"`
	DefaultDownloader  string `json:"defaultDownloader"`
	LibraryBackendMode string `json:"libraryBackendMode"`
}

func (s *Server) currentSettings(r *http.Request) settingsDTO {
	out := settingsDTO{AccentColor: defaultAccentColor, DynamicBackground: true}
	if s.deps.Adapters == nil {
		return out
	}
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyAccentColor); err == nil && v != "" {
		out.AccentColor = v
	}
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyDynamicBackground); err == nil {
		out.DynamicBackground = v != "false"
	}
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyDefaultDownloader); err == nil {
		out.DefaultDownloader = v
	}
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyLibraryBackendMode); err == nil && v != "" {
		out.LibraryBackendMode = v
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentSettings(r))
}

// putSettingsBody uses pointers so an omitted field is left unchanged.
type putSettingsBody struct {
	AccentColor        *string `json:"accentColor"`
	DynamicBackground  *bool   `json:"dynamicBackground"`
	DefaultDownloader  *string `json:"defaultDownloader"`
	LibraryBackendMode *string `json:"libraryBackendMode"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	var body putSettingsBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.AccentColor != nil {
		if !hexColorRE.MatchString(*body.AccentColor) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "accentColor must be a valid hex color (e.g. #F0354B)"})
			return
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyAccentColor, Value: *body.AccentColor}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	if body.DynamicBackground != nil {
		v := "true"
		if !*body.DynamicBackground {
			v = "false"
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyDynamicBackground, Value: v}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	if body.DefaultDownloader != nil {
		name := *body.DefaultDownloader
		if name != "" && !s.downloaderRegistered(name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "defaultDownloader must be empty or a registered downloader"})
			return
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyDefaultDownloader, Value: name}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	if body.LibraryBackendMode != nil {
		mode := *body.LibraryBackendMode
		if mode != "" && mode != "built-in" && mode != "external" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "libraryBackendMode must be empty, \"built-in\", or \"external\""})
			return
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyLibraryBackendMode, Value: mode}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
	writeJSON(w, http.StatusOK, s.currentSettings(r))
}

// downloaderRegistered reports whether name is a registered downloader adapter.
func (s *Server) downloaderRegistered(name string) bool {
	if s.deps.Downloader == nil {
		return false
	}
	for _, n := range s.deps.Downloader.Names() {
		if n == name {
			return true
		}
	}
	return false
}
