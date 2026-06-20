package main

import (
	"encoding/json"
	"log"

	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store/db"
)

// buildDownloaders instantiates every ENABLED adapter_instance of type
// "downloader" from the registry, applying env overrides (CRATE_SPOTDL_PATH →
// binary_path, CRATE_DOWNLOAD_DIR → output_dir) just before Init. instances are
// ordered by (type, priority) from ListAdapterInstances, so the returned slice is
// already in fallback-chain order. Per-source failures warn-and-skip.
func buildDownloaders(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []download.Downloader {
	out := []download.Downloader{}
	for i := range instances {
		inst := instances[i]
		if inst.Type != "downloader" || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: downloader %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		dl, ok := plugin.(download.Downloader)
		if !ok {
			log.Printf("WARNING: adapter %q is not a Downloader — skipping", inst.Name)
			continue
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: downloader %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		// Env overrides (spotdl) before Init.
		if inst.Name == "spotdl" {
			if p := getenv("CRATE_SPOTDL_PATH"); p != "" {
				cfg["binary_path"] = p
			}
			if d := getenv("CRATE_DOWNLOAD_DIR"); d != "" {
				cfg["output_dir"] = d
			}
		}

		if err := dl.Init(cfg); err != nil {
			log.Printf("WARNING: downloader %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, dl)
	}
	return out
}
