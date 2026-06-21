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
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("content-type = %q, want application/yaml", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"openapi: 3.0.3",
		"/version:",
		"/search/everywhere:",
		"/downloads:",
		"/ws:",
		"/stream/{id}:",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("spec missing %q", want)
		}
	}
}
