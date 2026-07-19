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

func TestDownloadWorkers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  int
	}{
		{name: "unset", want: 2},
		{name: "invalid", value: "fast", want: 2},
		{name: "zero", value: "0", want: 2},
		{name: "configured", value: "3", want: 3},
		{name: "capped", value: "99", want: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := downloadWorkers(func(string) string { return tc.value }); got != tc.want {
				t.Fatalf("downloadWorkers() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBuildLibraryAdapter_BuiltIn_IgnoresInstancesUsesLocalhost(t *testing.T) {
	// No library instances at all, built-in mode -> still get a configured adapter.
	lib, err := BuildLibraryAdapter(
		context.Background(), libReg(), nil, func(string) string { return "" },
		embedded.ModeBuiltIn, embedded.Credentials{Username: "admin", Password: "pw123456"},
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

func TestBuildLibraryAdapter_BuiltIn_UsesLocalhostURL(t *testing.T) {
	// Registers the capturing stubLib under "subsonic" so we can assert the exact
	// config map that BuildLibraryAdapter synthesizes for built-in mode.
	reg := registry.NewRegistry("library")
	captured := &stubLib{}
	reg.Register("subsonic", func() registry.Plugin { return captured })

	_, err := BuildLibraryAdapter(
		context.Background(), reg, nil /*no instances*/, func(string) string { return "" },
		embedded.ModeBuiltIn, embedded.Credentials{Username: "admin", Password: "pw123456"},
	)
	if err != nil {
		t.Fatalf("built-in build: %v", err)
	}
	if captured.got["url"] != "http://127.0.0.1:4533" {
		t.Errorf("url = %q, want http://127.0.0.1:4533", captured.got["url"])
	}
	if captured.got["username"] != "admin" {
		t.Errorf("username = %q, want admin", captured.got["username"])
	}
	if captured.got["password"] != "pw123456" {
		t.Errorf("password = %q, want pw", captured.got["password"])
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
