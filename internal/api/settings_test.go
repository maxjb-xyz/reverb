package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newRec() *httptest.ResponseRecorder { return httptest.NewRecorder() }
func newReq(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, bytes.NewBufferString(body))
}

func TestGetSettingsDefaults(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		AccentColor       string `json:"accentColor"`
		DynamicBackground bool   `json:"dynamicBackground"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccentColor != "#F0354B" {
		t.Fatalf("default accent should be #F0354B, got %q", body.AccentColor)
	}
	if !body.DynamicBackground {
		t.Fatal("dynamic_background should default to true")
	}
}

func TestPutThenGetSettings(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"accentColor":"#00FF88","dynamicBackground":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	var body struct {
		AccentColor       string `json:"accentColor"`
		DynamicBackground bool   `json:"dynamicBackground"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccentColor != "#00FF88" || body.DynamicBackground {
		t.Fatalf("round trip failed: %+v", body)
	}
}

func TestSettingsRequireAuth(t *testing.T) {
	srv, _ := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := newRec()
	srv.Handler().ServeHTTP(rec, newReq(http.MethodGet, "/api/v1/settings", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPutSettingsInvalidHex(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	rec := do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"accentColor":"notacolor"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid hex color", rec.Code)
	}
}

func TestPutSettingsPartialUpdate(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})
	// Set both first
	rec := do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"accentColor":"#AABBCC","dynamicBackground":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial put status = %d", rec.Code)
	}
	// Update only dynamicBackground
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"dynamicBackground":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial put status = %d: %s", rec.Code, rec.Body.String())
	}
	// accentColor should be preserved
	rec = do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	var body struct {
		AccentColor       string `json:"accentColor"`
		DynamicBackground bool   `json:"dynamicBackground"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccentColor != "#AABBCC" {
		t.Fatalf("accentColor should be preserved on partial update, got %q", body.AccentColor)
	}
	if !body.DynamicBackground {
		t.Fatal("dynamicBackground should be updated to true")
	}
}
