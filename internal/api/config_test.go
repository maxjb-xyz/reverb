package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPendingRestartReflectsFlag(t *testing.T) {
	dirty := &testDirty{}
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: dirty})

	get := func() bool {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil)
		req.AddCookie(cookie)
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var body struct {
			PendingRestart bool `json:"pendingRestart"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.PendingRestart
	}

	if get() {
		t.Fatal("should start clean")
	}
	dirty.Set()
	if !get() {
		t.Fatal("should be dirty after Set()")
	}
}

func TestPendingRestartNilSafe(t *testing.T) {
	srv, cookie := adapterTestServer(t, adapterServerOpts{dirty: nil})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
