package main

import (
	"encoding/json"
	"fmt"

	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/store/db"
)

// buildSearchSources instantiates every ENABLED adapter_instance of type
// "search" from the registry, applying CRATE_SPOTIFY_CLIENT_SECRET onto the
// spotify config_json just before Init (env wins; never sent to the browser).
// instances are already ordered by (type, priority) from ListAdapterInstances.
func buildSearchSources(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) ([]search.SearchSource, error) {
	out := []search.SearchSource{}
	for i := range instances {
		inst := instances[i]
		if inst.Type != "search" || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			return nil, fmt.Errorf("search source %q: %w", inst.Name, err)
		}
		src, ok := plugin.(search.SearchSource)
		if !ok {
			return nil, fmt.Errorf("adapter %q is not a SearchSource", inst.Name)
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				return nil, fmt.Errorf("search source %q config: %w", inst.Name, err)
			}
		}
		// Env secret override (Spotify) — env wins for client_secret before Init.
		if inst.Name == "spotify" {
			if sec := getenv("CRATE_SPOTIFY_CLIENT_SECRET"); sec != "" {
				cfg["client_secret"] = sec
			}
		}

		if err := src.Init(cfg); err != nil {
			return nil, fmt.Errorf("search source %q init: %w", inst.Name, err)
		}
		out = append(out, src)
	}
	return out, nil
}
