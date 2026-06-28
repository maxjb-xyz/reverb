package api

// TestGrainAlbumCapabilityProbe asserts that the "grain:album" capability probe
// (registered in main.go) causes DescribeCapabilities to include the capability
// for an album-granularity downloader (lidarr) and exclude it from a
// track-granularity one (spotdl). Placed here (not in registry_test.go) to avoid
// the registry → download → registry import cycle.
//
// TestAdapterDTOExposesCapabilities verifies that the adapter list endpoint
// returns a non-nil Capabilities slice in each DTO, so the FE can check
// capabilities.includes('grain:album') safely.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/download/lidarr"
	"github.com/maxjb-xyz/reverb/internal/download/spotdl"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

func TestGrainAlbumCapabilityProbe(t *testing.T) {
	// snapshot + restore global probes so other tests aren't polluted
	registry.SnapshotCapProbes()
	t.Cleanup(registry.RestoreCapProbes)

	registry.RegisterCapability("grain:album", func(p registry.Plugin) bool {
		d, ok := p.(download.Downloader)
		if !ok {
			return false
		}
		for _, g := range d.SupportedGranularities() {
			if g == core.GranularityAlbum {
				return true
			}
		}
		return false
	})

	t.Run("lidarr has grain:album", func(t *testing.T) {
		caps := registry.DescribeCapabilities(lidarr.New())
		found := false
		for _, c := range caps {
			if c == "grain:album" {
				found = true
			}
		}
		if !found {
			t.Fatalf("lidarr: want 'grain:album' in capabilities, got %v", caps)
		}
	})

	// spotDL now supports {track, album} granularities, so grain:album is present.
	t.Run("spotdl has grain:album", func(t *testing.T) {
		caps := registry.DescribeCapabilities(spotdl.New())
		found := false
		for _, c := range caps {
			if c == "grain:album" {
				found = true
			}
		}
		if !found {
			t.Fatalf("spotdl: want 'grain:album' in capabilities (SupportedGranularities includes album), got %v", caps)
		}
	})
}

func TestAdapterDTOExposesCapabilities(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})

	// Create an adapter instance.
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"t"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}

	// List and verify Capabilities is a non-nil array (not absent from JSON).
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}

	// Decode into a generic map to check the JSON key exists.
	var rawList []map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &rawList); err != nil {
		t.Fatal(err)
	}
	if len(rawList) != 1 {
		t.Fatalf("want 1 instance, got %d", len(rawList))
	}

	raw, ok := rawList[0]["capabilities"]
	if !ok {
		t.Fatal("'capabilities' key missing from adapter instance DTO")
	}
	// Must be a JSON array (not null).
	var caps []string
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatalf("capabilities is not a JSON array: %s — %v", string(raw), err)
	}

	// Also verify via the typed DTO: Capabilities must be non-nil.
	var list []adapterInstanceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list[0].Capabilities == nil {
		t.Fatal("adapterInstanceDTO.Capabilities must be a non-nil slice")
	}
}
