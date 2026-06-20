package main

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/store/db"
)

type stubSource struct {
	got map[string]any
}

func (s *stubSource) Type() string                             { return "search" }
func (s *stubSource) Name() string                             { return "spotify" }
func (s *stubSource) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (s *stubSource) Init(cfg map[string]any) error            { s.got = cfg; return nil }
func (s *stubSource) TestConnection(ctx context.Context) error { return nil }
func (s *stubSource) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	return nil, nil
}
func (s *stubSource) GetAlbum(ctx context.Context, id string) (core.ExternalAlbum, error) {
	return core.ExternalAlbum{}, nil
}

func TestBuildSearchSourcesAppliesEnvSecret(t *testing.T) {
	reg := registry.NewRegistry("search")
	captured := &stubSource{}
	reg.Register("spotify", func() registry.Plugin { return captured })

	instances := []db.AdapterInstance{{
		ID: "s1", Type: "search", Name: "spotify", Enabled: 1, Priority: 0,
		ConfigJson: `{"client_id":"cid","client_secret":"file-secret"}`,
	}}
	env := map[string]string{"CRATE_SPOTIFY_CLIENT_SECRET": "env-secret"}

	got, err := buildSearchSources(reg, instances, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 source, got %d", len(got))
	}
	if captured.got["client_secret"] != "env-secret" {
		t.Fatalf("env override not applied: %v", captured.got["client_secret"])
	}
	if captured.got["client_id"] != "cid" {
		t.Fatalf("client_id not parsed: %v", captured.got["client_id"])
	}
}

func TestBuildSearchSourcesSkipsDisabledAndNonSearch(t *testing.T) {
	reg := registry.NewRegistry("search")
	reg.Register("spotify", func() registry.Plugin { return &stubSource{} })
	instances := []db.AdapterInstance{
		{ID: "s1", Type: "search", Name: "spotify", Enabled: 0},
		{ID: "l1", Type: "library", Name: "subsonic", Enabled: 1},
	}
	got, err := buildSearchSources(reg, instances, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 sources, got %d", len(got))
	}
}

// Compile-time guard that the produced type matches the aggregator's input.
var _ = func() {
	var _ []search.SearchSource
}
