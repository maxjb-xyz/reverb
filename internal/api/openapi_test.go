package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesOpenAPI(t *testing.T) {
	srv := NewServer(Deps{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "openapi: 3.0.3") {
		t.Fatal("spec body not served")
	}
}
