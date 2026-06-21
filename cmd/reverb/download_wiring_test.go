package main

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/download/spotdl"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestBuildDownloadersEnabledOnly(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	instances := []db.AdapterInstance{
		{Type: "downloader", Name: "spotdl", Enabled: 1, ConfigJson: `{"output_dir":"/music"}`},
		{Type: "downloader", Name: "spotdl", Enabled: 0, ConfigJson: `{"output_dir":"/music2"}`},
		{Type: "library", Name: "subsonic", Enabled: 1, ConfigJson: `{}`},
	}
	out := buildDownloaders(reg, instances, env(nil))
	if len(out) != 1 {
		t.Fatalf("want 1 enabled downloader, got %d", len(out))
	}
	if out[0].Name() != "spotdl" {
		t.Fatalf("name = %q", out[0].Name())
	}
}

func TestBuildDownloadersEnvOverrideAndSkipOnBadConfig(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	instances := []db.AdapterInstance{
		// Missing output_dir in config; env supplies it → must succeed.
		{Type: "downloader", Name: "spotdl", Enabled: 1, ConfigJson: `{}`},
		// Unknown adapter → warn-and-skip, not a panic.
		{Type: "downloader", Name: "ghost", Enabled: 1, ConfigJson: `{}`},
	}
	out := buildDownloaders(reg, instances, env(map[string]string{"REVERB_DOWNLOAD_DIR": "/from/env"}))
	if len(out) != 1 {
		t.Fatalf("want 1 downloader (env-supplied dir), got %d", len(out))
	}
}

func TestBuildDownloadersBundledSpotdlDefault(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	// No downloader instance configured + REVERB_DOWNLOAD_DIR set (as the image
	// sets it) → the bundled spotDL default is injected.
	instances := []db.AdapterInstance{{Type: "library", Name: "subsonic", Enabled: 1, ConfigJson: `{}`}}
	out := buildDownloaders(reg, instances, env(map[string]string{"REVERB_DOWNLOAD_DIR": "/music"}))
	if len(out) != 1 || out[0].Name() != "spotdl" {
		t.Fatalf("want 1 bundled spotdl default, got %d", len(out))
	}
}

func TestBuildDownloadersNoDefaultWhenInstancePresent(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	// A DISABLED downloader instance means the user manages it → do NOT inject the
	// bundled default even though no downloader ends up enabled.
	instances := []db.AdapterInstance{
		{Type: "downloader", Name: "spotdl", Enabled: 0, ConfigJson: `{"output_dir":"/music"}`},
	}
	out := buildDownloaders(reg, instances, env(map[string]string{"REVERB_DOWNLOAD_DIR": "/music"}))
	if len(out) != 0 {
		t.Fatalf("want 0 (respect user's disabled instance), got %d", len(out))
	}
}

func TestBuildDownloadersNoDefaultWithoutDir(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	// No env (e.g. local dev) → no bundled default, unchanged behavior.
	out := buildDownloaders(reg, nil, env(nil))
	if len(out) != 0 {
		t.Fatalf("want 0 without REVERB_DOWNLOAD_DIR, got %d", len(out))
	}
}
