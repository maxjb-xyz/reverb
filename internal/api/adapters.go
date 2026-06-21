package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// adapterInstanceDTO is the browser-facing shape of a configured adapter instance.
// Config has Secret:true fields redacted (value removed, "<key>__isSet" boolean added).
type adapterInstanceDTO struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

// createAdapterBody / updateAdapterBody are the request DTOs.
type createAdapterBody struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

type updateAdapterBody struct {
	Name     string         `json:"name"`
	Enabled  bool           `json:"enabled"`
	Priority int            `json:"priority"`
	Config   map[string]any `json:"config"`
}

// registries returns the three registries in a stable order for lookup.
func (s *Server) registries() []*registry.Registry {
	return []*registry.Registry{s.deps.Lib, s.deps.Search, s.deps.Downloader}
}

// schemaFor finds the ConfigSchema for an adapter name across all registries.
// Returns an empty schema if the name is not registered (redaction still safe).
func (s *Server) schemaFor(name string) registry.ConfigSchema {
	for _, reg := range s.registries() {
		if reg == nil {
			continue
		}
		for _, n := range reg.Names() {
			if n != name {
				continue
			}
			if p, err := reg.Create(n); err == nil {
				return p.ConfigSchema()
			}
		}
	}
	return registry.ConfigSchema{}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// toDTO converts a stored row into the redacted browser DTO.
func (s *Server) toDTO(inst db.AdapterInstance) adapterInstanceDTO {
	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		_ = json.Unmarshal([]byte(inst.ConfigJson), &cfg)
	}
	return adapterInstanceDTO{
		ID:       inst.ID,
		Type:     inst.Type,
		Name:     inst.Name,
		Enabled:  inst.Enabled == 1,
		Priority: int(inst.Priority),
		Config:   redactConfig(s.schemaFor(inst.Name), cfg),
	}
}

func (s *Server) handleListAdapters(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusOK, []adapterInstanceDTO{})
		return
	}
	rows, err := s.deps.Adapters.ListAdapterInstances(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list adapters"})
		return
	}
	out := make([]adapterInstanceDTO, 0, len(rows))
	for _, inst := range rows {
		out = append(out, s.toDTO(inst))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAdapter(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	var body createAdapterBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.Type == "" || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type and name are required"})
		return
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	// New instance: no stored secrets to preserve; just strip any __isSet sidecars.
	persist := mergeSecrets(s.schemaFor(body.Name), map[string]any{}, body.Config)
	cfgJSON, _ := json.Marshal(persist)
	id := uuid.NewString()
	if err := s.deps.Adapters.CreateAdapterInstance(r.Context(), db.CreateAdapterInstanceParams{
		ID: id, Type: body.Type, Name: body.Name,
		Enabled: boolToInt(body.Enabled), Priority: int64(body.Priority), ConfigJson: string(cfgJSON),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create adapter"})
		return
	}
	// Apply live: rebuild and swap the active services. The DB change has already
	// persisted; a rebuild failure is logged but still reported as success.
	if err := s.reload(r.Context()); err != nil {
		log.Printf("WARNING: adapter create persisted but live reload failed: %v", err)
	}
	inst, _ := s.deps.Adapters.GetAdapterInstance(r.Context(), id)
	writeJSONPending(w, http.StatusCreated, s.toDTO(inst), false)
}

func (s *Server) handleUpdateAdapter(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := s.deps.Adapters.GetAdapterInstance(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "adapter not found"})
		return
	}
	var body updateAdapterBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	stored := map[string]any{}
	if existing.ConfigJson != "" {
		_ = json.Unmarshal([]byte(existing.ConfigJson), &stored)
	}
	name := body.Name
	if name == "" {
		name = existing.Name
	}
	persist := mergeSecrets(s.schemaFor(name), stored, body.Config)
	cfgJSON, _ := json.Marshal(persist)
	if err := s.deps.Adapters.UpdateAdapterInstance(r.Context(), db.UpdateAdapterInstanceParams{
		Name: name, Enabled: boolToInt(body.Enabled), Priority: int64(body.Priority),
		ConfigJson: string(cfgJSON), ID: id,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update adapter"})
		return
	}
	if err := s.reload(r.Context()); err != nil {
		log.Printf("WARNING: adapter update persisted but live reload failed: %v", err)
	}
	inst, _ := s.deps.Adapters.GetAdapterInstance(r.Context(), id)
	writeJSONPending(w, http.StatusOK, s.toDTO(inst), false)
}

func (s *Server) handleDeleteAdapter(w http.ResponseWriter, r *http.Request) {
	if s.deps.Adapters == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config store unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.deps.Adapters.DeleteAdapterInstance(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not delete adapter"})
		return
	}
	if err := s.reload(r.Context()); err != nil {
		log.Printf("WARNING: adapter delete persisted but live reload failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pendingRestart": false})
}

type testAdapterBody struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config"`
}

// createUnregistered finds and instantiates a NON-persisted adapter by name across
// all registries. Returns (nil, false) if the name is not registered.
func (s *Server) createUnregistered(name string) (registry.Plugin, bool) {
	for _, reg := range s.registries() {
		if reg == nil {
			continue
		}
		for _, n := range reg.Names() {
			if n == name {
				if p, err := reg.Create(n); err == nil {
					return p, true
				}
			}
		}
	}
	return nil, false
}

// overlayEnvSecrets applies the same env secret overrides the composition root uses,
// so a Test honors REVERB_* secrets when the form omits them. Mirrors *_wiring.go.
func overlayEnvSecrets(name string, cfg map[string]any) {
	switch name {
	case "subsonic":
		if v := os.Getenv("REVERB_LIBRARY_PASSWORD"); v != "" {
			cfg["password"] = v
		}
	case "spotify":
		if v := os.Getenv("REVERB_SPOTIFY_CLIENT_SECRET"); v != "" {
			cfg["client_secret"] = v
		}
	case "spotdl":
		if v := os.Getenv("REVERB_SPOTDL_PATH"); v != "" {
			cfg["binary_path"] = v
		}
		if v := os.Getenv("REVERB_DOWNLOAD_DIR"); v != "" {
			cfg["output_dir"] = v
		}
	}
}

func (s *Server) handleTestAdapter(w http.ResponseWriter, r *http.Request) {
	var body testAdapterBody
	if err := decode(r, &body); err != nil || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	plugin, ok := s.createUnregistered(body.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown adapter: " + body.Name})
		return
	}
	cfg := body.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	// Strip any __isSet sidecars the client may echo; never feed them to Init.
	for k := range cfg {
		if len(k) > len(isSetSuffix) && k[len(k)-len(isSetSuffix):] == isSetSuffix {
			delete(cfg, k)
		}
	}
	overlayEnvSecrets(body.Name, cfg)

	if err := plugin.Init(cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := plugin.TestConnection(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
