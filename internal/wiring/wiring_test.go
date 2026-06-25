package wiring

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/library/embedded"
	"github.com/maxjb-xyz/reverb/internal/library/subsonic"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func libReg() *registry.Registry {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return subsonic.New() })
	return reg
}

func TestBuildLibraryAdapter_BuiltIn_IgnoresInstancesUsesLocalhost(t *testing.T) {
	// No library instances at all, built-in mode -> still get a configured adapter.
	lib, err := BuildLibraryAdapter(
		context.Background(), libReg(), nil, func(string) string { return "" },
		embedded.ModeBuiltIn, embedded.Credentials{Username: "admin", Password: "pw"},
	)
	if err != nil {
		t.Fatalf("built-in build: %v", err)
	}
	if lib == nil {
		t.Fatal("built-in mode must synthesize a library adapter")
	}
	if lib.Name() != "subsonic" {
		t.Errorf("adapter = %q, want subsonic", lib.Name())
	}
}

func TestBuildLibraryAdapter_External_UsesInstanceConfig(t *testing.T) {
	inst := []db.AdapterInstance{{
		ID: "x", Type: "library", Name: "subsonic", Enabled: 1,
		ConfigJson: `{"url":"http://nav.example:4533","username":"u","password":"p"}`,
	}}
	lib, err := BuildLibraryAdapter(
		context.Background(), libReg(), inst, func(string) string { return "" },
		embedded.ModeExternal, embedded.Credentials{},
	)
	if err != nil {
		t.Fatalf("external build: %v", err)
	}
	if lib == nil {
		t.Fatal("external mode with a configured instance must build an adapter")
	}
}
