package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

var errFakeConn = errors.New("connection refused")

func TestCreateThenListRedactsSecret(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})

	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"shh"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	// Adapter changes apply live (no restart): the config-dirty flag must NOT be
	// flipped and the response must report pendingRestart=false.
	if dirty.Dirty() {
		t.Fatal("create must NOT flip the config-dirty flag (changes apply live)")
	}
	var createResp struct {
		PendingRestart bool `json:"pendingRestart"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &createResp)
	if createResp.PendingRestart {
		t.Fatal("pendingRestart must be false (changes apply live)")
	}

	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list []adapterInstanceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 instance, got %d", len(list))
	}
	cfg := list[0].Config
	if cfg["url"] != "http://x" {
		t.Fatalf("non-secret should be visible, got %v", cfg["url"])
	}
	if _, present := cfg["token"]; present {
		t.Fatalf("secret VALUE must NOT be returned, got %v", cfg["token"])
	}
	if cfg["token__isSet"] != true {
		t.Fatalf("expected token__isSet=true, got %v", cfg["token__isSet"])
	}
}

func TestUpdatePreservesSecretWhenBlank(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"priority":0,"config":{"url":"http://x","token":"orig"}}`)
	var wrap struct{ Data adapterInstanceDTO `json:"data"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &wrap)
	created := wrap.Data

	// Update with a blank token → must preserve "orig".
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/adapters/"+created.ID,
		`{"name":"fake","enabled":true,"priority":3,"config":{"url":"http://y","token":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", rec.Code, rec.Body.String())
	}

	// Read the raw stored config_json via the store to assert the secret survived.
	inst, err := getStoredInstance(t, srv, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	_ = json.Unmarshal([]byte(inst), &stored)
	if stored["token"] != "orig" {
		t.Fatalf("blank update must preserve stored secret, got %v", stored["token"])
	}
	if stored["url"] != "http://y" {
		t.Fatalf("non-secret should update, got %v", stored["url"])
	}
}

func TestUpdateNewSecretOverwrites(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","enabled":true,"config":{"url":"http://x","token":"orig"}}`)
	var wrap struct{ Data adapterInstanceDTO `json:"data"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &wrap)
	created := wrap.Data

	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/adapters/"+created.ID,
		`{"name":"fake","enabled":true,"config":{"url":"http://x","token":"changed"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	inst, _ := getStoredInstance(t, srv, created.ID)
	var stored map[string]any
	_ = json.Unmarshal([]byte(inst), &stored)
	if stored["token"] != "changed" {
		t.Fatalf("new secret must overwrite, got %v", stored["token"])
	}
}

func TestDeleteAdapter(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters",
		`{"type":"search","name":"fake","config":{"url":"http://x","token":"t"}}`)
	var wrap struct{ Data adapterInstanceDTO `json:"data"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &wrap)
	created := wrap.Data

	rec = do(t, srv, cookie, http.MethodDelete, "/api/v1/adapters/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/adapters", "")
	var list []adapterInstanceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("want 0 after delete, got %d", len(list))
	}
}

func TestAdaptersRequireAuth(t *testing.T) {
	srv, _ := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/adapters", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestTestAdapterOK(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}, testErr: nil})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"fake","config":{"url":"http://x","token":"t"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.OK {
		t.Fatalf("expected ok=true, got %+v", body)
	}
}

func TestTestAdapterError(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}, testErr: errFakeConn})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"fake","config":{"url":"http://x","token":"t"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (a connection failure is still a 200 ok:false)", rec.Code)
	}
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.OK || body.Error == "" {
		t.Fatalf("expected ok=false with error, got %+v", body)
	}
}

func TestTestAdapterUnknownName(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/adapters/test",
		`{"name":"nope","config":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown adapter", rec.Code)
	}
}

// TestAllProtectedRoutesRequireAuth verifies that every new protected route
// returns 401 when no session cookie is present.
func TestAllProtectedRoutesRequireAuth(t *testing.T) {
	srv, _ := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})

	// Use a placeholder UUID so the router can parse {id} even though it won't exist.
	const fakeID = "00000000-0000-0000-0000-000000000001"

	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/adapters", ""},
		{http.MethodPost, "/api/v1/adapters", `{"type":"search","name":"fake","config":{}}`},
		{http.MethodPut, "/api/v1/adapters/" + fakeID, `{"name":"fake","config":{}}`},
		{http.MethodDelete, "/api/v1/adapters/" + fakeID, ""},
		{http.MethodPost, "/api/v1/adapters/test", `{"name":"fake","config":{}}`},
		{http.MethodGet, "/api/v1/settings", ""},
		{http.MethodPut, "/api/v1/settings", `{"accentColor":"#FF0000"}`},
		{http.MethodGet, "/api/v1/config/pending-restart", ""},
	}

	for _, tc := range routes {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var buf *bytes.Buffer
			if tc.body != "" {
				buf = bytes.NewBufferString(tc.body)
			} else {
				buf = bytes.NewBufferString("")
			}
			req := httptest.NewRequest(tc.method, tc.path, buf)
			// deliberately no cookie
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s: got %d, want 401", tc.method, tc.path, rec.Code)
			}
		})
	}
}

// getStoredInstance reads the RAW config_json for an instance from the server's
// store (test helper). It uses the AdapterStore on Deps via a small accessor.
func getStoredInstance(t *testing.T, srv *Server, id string) (string, error) {
	t.Helper()
	inst, err := srv.deps.Adapters.GetAdapterInstance(context.Background(), id)
	if err != nil {
		return "", err
	}
	return inst.ConfigJson, nil
}
