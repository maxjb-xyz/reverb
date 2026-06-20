package main

import (
	"context"
	"errors"
	"testing"

	"github.com/maximusjb/crate/internal/library"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store/db"
)

// stubLib captures the config passed to Init so we can assert env override + parse.
type stubLib struct {
	got map[string]any
	library.LibraryAdapter
}

func (s *stubLib) Type() string                             { return "library" }
func (s *stubLib) Name() string                             { return "subsonic" }
func (s *stubLib) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (s *stubLib) Init(cfg map[string]any) error            { s.got = cfg; return nil }
func (s *stubLib) TestConnection(ctx context.Context) error { return nil }

func TestBuildLibraryAdapterAppliesEnvSecret(t *testing.T) {
	reg := registry.NewRegistry("library")
	captured := &stubLib{}
	reg.Register("subsonic", func() registry.Plugin { return captured })

	instances := []db.AdapterInstance{{
		ID: "i1", Type: "library", Name: "subsonic", Enabled: 1, Priority: 0,
		ConfigJson: `{"url":"http://nav:4533","username":"alice","password":"file-pw"}`,
	}}
	env := map[string]string{"CRATE_LIBRARY_PASSWORD": "env-pw"}

	got, err := buildLibraryAdapter(context.Background(), reg, instances, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected an adapter")
	}
	if captured.got["password"] != "env-pw" {
		t.Fatalf("env override not applied: %v", captured.got["password"])
	}
	if captured.got["url"] != "http://nav:4533" {
		t.Fatalf("url not parsed: %v", captured.got["url"])
	}
}

func TestBuildLibraryAdapterNoEnabledInstance(t *testing.T) {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return &stubLib{} })
	instances := []db.AdapterInstance{{ID: "i1", Type: "library", Name: "subsonic", Enabled: 0}}
	got, err := buildLibraryAdapter(context.Background(), reg, instances, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil when no enabled library instance")
	}
}

func TestBuildLibraryAdapterIgnoresNonLibraryTypes(t *testing.T) {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return &stubLib{} })
	instances := []db.AdapterInstance{{ID: "i1", Type: "search", Name: "spotify", Enabled: 1}}
	got, _ := buildLibraryAdapter(context.Background(), reg, instances, func(string) string { return "" })
	if got != nil {
		t.Fatal("expected nil — only library type counts")
	}
}

// stubLibInitFails is a variant of stubLib whose Init always returns an error.
type stubLibInitFails struct {
	library.LibraryAdapter
}

func (s *stubLibInitFails) Type() string                             { return "library" }
func (s *stubLibInitFails) Name() string                             { return "subsonic" }
func (s *stubLibInitFails) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (s *stubLibInitFails) Init(cfg map[string]any) error            { return errors.New("boom") }
func (s *stubLibInitFails) TestConnection(ctx context.Context) error { return nil }

func TestBuildLibraryAdapterInitFails(t *testing.T) {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return &stubLibInitFails{} })

	instances := []db.AdapterInstance{{
		ID: "i1", Type: "library", Name: "subsonic", Enabled: 1, Priority: 0,
		ConfigJson: `{"url":"http://nav:4533","username":"alice","password":"secret"}`,
	}}

	got, err := buildLibraryAdapter(context.Background(), reg, instances, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected an error from Init failure, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil adapter on Init error, got %v", got)
	}
}
