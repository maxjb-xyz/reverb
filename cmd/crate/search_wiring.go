package main

import (
	"encoding/json"
	"log"

	"github.com/maxjb-xyz/crate/internal/registry"
	"github.com/maxjb-xyz/crate/internal/search"
	"github.com/maxjb-xyz/crate/internal/store/db"
)

// buildSearchSources instantiates every ENABLED adapter_instance of type
// "search" from the registry, applying CRATE_SPOTIFY_CLIENT_SECRET onto the
// spotify config_json just before Init (env wins; never sent to the browser).
// instances are already ordered by (type, priority) from ListAdapterInstances.
func buildSearchSources(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []search.SearchSource {
	out := []search.SearchSource{}
	for i := range instances {
		inst := instances[i]
		if inst.Type != "search" || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: search source %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		src, ok := plugin.(search.SearchSource)
		if !ok {
			log.Printf("WARNING: adapter %q is not a SearchSource — skipping", inst.Name)
			continue
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: search source %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		// Env secret override (Spotify) — env wins for client_secret before Init.
		if inst.Name == "spotify" {
			if sec := getenv("CRATE_SPOTIFY_CLIENT_SECRET"); sec != "" {
				cfg["client_secret"] = sec
			}
		}

		if err := src.Init(cfg); err != nil {
			log.Printf("WARNING: search source %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, src)
	}
	return out
}
