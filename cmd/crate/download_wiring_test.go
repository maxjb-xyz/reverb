package main

import (
	"testing"

	"github.com/maximusjb/crate/internal/download/spotdl"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store/db"
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
	out := buildDownloaders(reg, instances, env(map[string]string{"CRATE_DOWNLOAD_DIR": "/from/env"}))
	if len(out) != 1 {
		t.Fatalf("want 1 downloader (env-supplied dir), got %d", len(out))
	}
}
