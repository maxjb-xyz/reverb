package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMeIncludesCreatedAt(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")
	rr := doGET(t, srv, "/api/v1/me", tok)
	if rr.Code != 200 {
		t.Fatalf("GET /me = %d (%s)", rr.Code, rr.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /me body: %v", err)
	}
	v, ok := body["createdAt"]
	if !ok {
		t.Fatalf("/me response missing createdAt field: %s", rr.Body)
	}
	// createdAt is a JSON number; json.Unmarshal decodes it as float64
	ts, _ := v.(float64)
	if ts <= 0 {
		t.Fatalf("/me createdAt = %v, want non-zero unix timestamp", v)
	}
}

func TestHealth(t *testing.T) {
	srv := NewServer(Deps{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", body["status"])
	}
}
