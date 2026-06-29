package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store"
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

// TestDefaultDownloaderSettingRemoved asserts that GET /settings no longer
// includes a "defaultDownloader" key and that PUT /settings silently ignores
// any "defaultDownloader" field sent by old clients (i.e. returns 200 without
// storing or reflecting the value).
func TestDefaultDownloaderSettingRemoved(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: &testDirty{}})

	// GET must not include "defaultDownloader" key at all.
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings status = %d", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := raw["defaultDownloader"]; present {
		t.Fatal("GET /settings must NOT include defaultDownloader key")
	}

	// PUT with a "defaultDownloader" field must be silently ignored (200, no error).
	rec = do(t, srv, cookie, http.MethodPut, "/api/v1/settings",
		`{"defaultDownloader":"spotdl","accentColor":"#112233"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT with defaultDownloader = %d, want 200", rec.Code)
	}
	// Confirm the response also has no defaultDownloader key.
	var raw2 map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw2); err != nil {
		t.Fatalf("unmarshal put response: %v", err)
	}
	if _, present := raw2["defaultDownloader"]; present {
		t.Fatal("PUT /settings response must NOT include defaultDownloader key")
	}
}

// TestMaxPendingRequestsPerUserSetting verifies the maxPendingRequestsPerUser
// field in GET /settings (default 0 when unset) and that PUT /settings persists
// it and round-trips (admin). Also validates non-negative enforcement.
func TestMaxPendingRequestsPerUserSetting(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/quota.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{Auth: authSvc, Adapters: st.Q()})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	// GET default → maxPendingRequestsPerUser must be 0.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings status = %d", rec.Code)
	}
	var dto struct {
		MaxPendingRequestsPerUser int `json:"maxPendingRequestsPerUser"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dto.MaxPendingRequestsPerUser != 0 {
		t.Fatalf("default maxPendingRequestsPerUser = %d, want 0", dto.MaxPendingRequestsPerUser)
	}

	// PUT {maxPendingRequestsPerUser:3} → 200.
	putRec := httptest.NewRecorder()
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings",
		bytes.NewBufferString(`{"maxPendingRequestsPerUser":3}`))
	putReq.AddCookie(cookie)
	srv.Handler().ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /settings status = %d: %s", putRec.Code, putRec.Body.String())
	}

	// GET after PUT → must round-trip to 3.
	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	getReq.AddCookie(cookie)
	srv.Handler().ServeHTTP(getRec, getReq)
	var dto2 struct {
		MaxPendingRequestsPerUser int `json:"maxPendingRequestsPerUser"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &dto2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dto2.MaxPendingRequestsPerUser != 3 {
		t.Fatalf("after PUT maxPendingRequestsPerUser = %d, want 3", dto2.MaxPendingRequestsPerUser)
	}

	// PUT with negative value → 400.
	negRec := httptest.NewRecorder()
	negReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings",
		bytes.NewBufferString(`{"maxPendingRequestsPerUser":-1}`))
	negReq.AddCookie(cookie)
	srv.Handler().ServeHTTP(negRec, negReq)
	if negRec.Code != http.StatusBadRequest {
		t.Fatalf("PUT negative maxPendingRequestsPerUser = %d, want 400", negRec.Code)
	}
}

func TestLibraryBackendModeSetting(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{Auth: authSvc, Adapters: st.Q()})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	put := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(body))
		req.AddCookie(cookie)
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := put(`{"libraryBackendMode":"external"}`); code != http.StatusOK {
		t.Fatalf("set external = %d", code)
	}
	if code := put(`{"libraryBackendMode":"built-in"}`); code != http.StatusOK {
		t.Fatalf("set built-in = %d", code)
	}
	if code := put(`{"libraryBackendMode":"bogus"}`); code != http.StatusBadRequest {
		t.Fatalf("bogus mode = %d, want 400", code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	var dto struct {
		LibraryBackendMode string `json:"libraryBackendMode"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.LibraryBackendMode != "built-in" {
		t.Fatalf("mode = %q, want built-in", dto.LibraryBackendMode)
	}
}
