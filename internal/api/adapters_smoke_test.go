package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdapterLifecycleSmoke(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty, testErr: nil})

	// 1. create
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"sekret"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data adapterInstanceDTO `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Data.ID
	if id == "" {
		t.Fatal("no id returned")
	}

	// 2. list redacts the secret
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	var list []adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Config["token__isSet"] != true {
		t.Fatalf("list redaction failed: %+v", list)
	}
	if _, leaked := list[0].Config["token"]; leaked {
		t.Fatal("secret leaked in list")
	}

	// 3. test ok
	rec = do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"fake","config":{"url":"http://x","token":"sekret"}}`)
	var test struct{ OK bool `json:"ok"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &test)
	if !test.OK {
		t.Fatalf("test should be ok, got %s", rec.Body.String())
	}

	// 4. update with blank secret preserves it
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/adapters/"+id,
		`{"name":"fake","enabled":true,"priority":2,"config":{"url":"http://y","token":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d", rec.Code)
	}
	raw, _ := getStoredInstance(t, srv, id)
	var stored map[string]any
	_ = json.Unmarshal([]byte(raw), &stored)
	if stored["token"] != "sekret" || stored["url"] != "http://y" {
		t.Fatalf("preserve+update failed: %+v", stored)
	}

	// 5. delete
	rec = do(t, srv, cookie, http.MethodDelete, "/api/v1/adapters/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	list = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("want empty after delete, got %d", len(list))
	}

	// 6. config is dirty (restart-to-apply)
	if !dirty.Dirty() {
		t.Fatal("config should be dirty after mutations")
	}

	// 7. settings GET returns defaults
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("settings GET = %d", rec.Code)
	}
	var settings struct {
		AccentColor       string `json:"accentColor"`
		DynamicBackground bool   `json:"dynamicBackground"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &settings)
	if settings.AccentColor != defaultAccentColor {
		t.Fatalf("default accent = %q, want %q", settings.AccentColor, defaultAccentColor)
	}
	if !settings.DynamicBackground {
		t.Fatal("dynamicBackground should default to true")
	}

	// 8. settings PUT round-trip
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"accentColor":"#112233","dynamicBackground":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings PUT = %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &settings)
	if settings.AccentColor != "#112233" || settings.DynamicBackground {
		t.Fatalf("settings round-trip failed: %+v", settings)
	}
}
