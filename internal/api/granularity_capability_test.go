package api

// TestGrainAlbumCapabilityProbe was removed: the "grain:album" capability probe
// has been retired. Granularity information is now exposed directly on the adapter
// instance DTO via SupportedGranularities ([]string) and Granularities (map[string]int).
// See TestAdapterDTOGranularities* in adapters_test.go.

import (
	"encoding/json"
	"net/http"
	"testing"
)

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
