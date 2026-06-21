package main

import (
	"encoding/json"
	"log"

	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// buildDownloaders instantiates every ENABLED adapter_instance of type
// "downloader" from the registry, applying env overrides (REVERB_SPOTDL_PATH →
// binary_path, REVERB_DOWNLOAD_DIR → output_dir) just before Init. instances are
// ordered by (type, priority) from ListAdapterInstances, so the returned slice is
// already in fallback-chain order. Per-source failures warn-and-skip.
func buildDownloaders(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []download.Downloader {
	out := []download.Downloader{}
	hasDownloaderInstance := false
	for i := range instances {
		inst := instances[i]
		if inst.Type != "downloader" {
			continue
		}
		hasDownloaderInstance = true
		if inst.Enabled != 1 {
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
			if p := getenv("REVERB_SPOTDL_PATH"); p != "" {
				cfg["binary_path"] = p
			}
			if d := getenv("REVERB_DOWNLOAD_DIR"); d != "" {
				cfg["output_dir"] = d
			}
		}

		if err := dl.Init(cfg); err != nil {
			log.Printf("WARNING: downloader %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, dl)
	}

	// Bundled default: the image ships spotDL + ffmpeg, so when the user has not
	// configured any downloader, fall back to a spotDL instance writing to
	// REVERB_DOWNLOAD_DIR (the Docker image sets this to /music). This makes
	// downloads work out of the box with zero setup. We only inject the default
	// when there is NO downloader instance at all — if the user configured (or
	// deliberately disabled) one, that choice is respected. Gated on the env being
	// set so local/dev runs without it are unaffected.
	if len(out) == 0 && !hasDownloaderInstance {
		if dir := getenv("REVERB_DOWNLOAD_DIR"); dir != "" {
			if dl := buildDefaultSpotdl(reg, dir, getenv); dl != nil {
				out = append(out, dl)
			}
		}
	}
	return out
}

// buildDefaultSpotdl constructs the bundled spotDL downloader (output_dir=dir).
// Returns nil (with a log line) if spotDL can't be created/initialised, e.g. a
// build/registry without it — never fatal.
func buildDefaultSpotdl(reg *registry.Registry, dir string, getenv func(string) string) download.Downloader {
	plugin, err := reg.Create("spotdl")
	if err != nil {
		log.Printf("bundled spotdl downloader unavailable: %v", err)
		return nil
	}
	dl, ok := plugin.(download.Downloader)
	if !ok {
		return nil
	}
	cfg := map[string]any{"output_dir": dir}
	if p := getenv("REVERB_SPOTDL_PATH"); p != "" {
		cfg["binary_path"] = p
	}
	if err := dl.Init(cfg); err != nil {
		log.Printf("bundled spotdl downloader unavailable: %v", err)
		return nil
	}
	log.Printf("using bundled spotdl downloader (output_dir=%s)", dir)
	return dl
}
