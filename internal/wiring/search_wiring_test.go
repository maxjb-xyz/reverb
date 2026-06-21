package wiring

import (
	"context"
	"errors"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
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

// stubFailSource is a stubSource whose Init always returns an error.
type stubFailSource struct {
	stubSource
}

func (s *stubFailSource) Init(_ map[string]any) error { return errors.New("init failure") }

// compile-time assertion: BuildSearchSources returns the aggregator's expected input type.
var _ []search.SearchSource = BuildSearchSources(nil, nil, nil)

func TestBuildSearchSourcesAppliesEnvSecret(t *testing.T) {
	reg := registry.NewRegistry("search")
	captured := &stubSource{}
	reg.Register("spotify", func() registry.Plugin { return captured })

	instances := []db.AdapterInstance{{
		ID: "s1", Type: "search", Name: "spotify", Enabled: 1, Priority: 0,
		ConfigJson: `{"client_id":"cid","client_secret":"file-secret"}`,
	}}
	env := map[string]string{"REVERB_SPOTIFY_CLIENT_SECRET": "env-secret"}

	got := BuildSearchSources(reg, instances, func(k string) string { return env[k] })
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
	got := BuildSearchSources(reg, instances, func(string) string { return "" })
	if len(got) != 0 {
		t.Fatalf("want 0 sources, got %d", len(got))
	}
}

func TestBuildSearchSourcesInitFailSkips(t *testing.T) {
	reg := registry.NewRegistry("search")
	// First source: Init always fails.
	reg.Register("bad-source", func() registry.Plugin { return &stubFailSource{} })
	// Second source: Init succeeds.
	good := &stubSource{}
	reg.Register("good-source", func() registry.Plugin { return good })

	instances := []db.AdapterInstance{
		{ID: "b1", Type: "search", Name: "bad-source", Enabled: 1},
		{ID: "g1", Type: "search", Name: "good-source", Enabled: 1},
	}

	got := BuildSearchSources(reg, instances, func(string) string { return "" })
	if len(got) != 1 {
		t.Fatalf("want 1 source (good-source only), got %d", len(got))
	}
	if got[0] != good {
		t.Fatalf("expected good-source to be the surviving source")
	}
}
