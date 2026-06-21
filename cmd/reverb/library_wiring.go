package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// buildLibraryAdapter builds the active LibraryAdapter from the first enabled
// adapter_instance of type "library". It applies env secret overrides
// (REVERB_LIBRARY_PASSWORD) onto the stored config_json before Init. The library
// is optional: with no enabled library instance it returns (nil, nil).
func buildLibraryAdapter(
	ctx context.Context,
	reg *registry.Registry,
	instances []db.AdapterInstance,
	getenv func(string) string,
) (library.LibraryAdapter, error) {
	var inst *db.AdapterInstance
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			inst = &instances[i]
			break
		}
	}
	if inst == nil {
		return nil, nil
	}

	plugin, err := reg.Create(inst.Name)
	if err != nil {
		return nil, fmt.Errorf("library adapter %q: %w", inst.Name, err)
	}
	lib, ok := plugin.(library.LibraryAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter %q is not a LibraryAdapter", inst.Name)
	}

	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
			return nil, fmt.Errorf("library adapter %q config: %w", inst.Name, err)
		}
	}
	// Env secret override — env wins for the password just before Init().
	if pw := getenv("REVERB_LIBRARY_PASSWORD"); pw != "" {
		cfg["password"] = pw
	}

	if err := lib.Init(cfg); err != nil {
		return nil, fmt.Errorf("library adapter %q init: %w", inst.Name, err)
	}
	return lib, nil
}
