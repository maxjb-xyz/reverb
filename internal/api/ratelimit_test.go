package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(2, time.Minute, func() time.Time { return now })

	if !rl.allow("a") {
		t.Fatal("first attempt should be allowed")
	}
	if !rl.allow("a") {
		t.Fatal("second attempt should be allowed")
	}
	if rl.allow("a") {
		t.Fatal("third attempt within the window should be blocked")
	}
	// A different key has its own budget.
	if !rl.allow("b") {
		t.Fatal("distinct key should be allowed")
	}
	// After the window elapses, the key resets.
	now = now.Add(61 * time.Second)
	if !rl.allow("a") {
		t.Fatal("attempt after the window resets should be allowed")
	}
}

// TestLoginRateLimited asserts the login endpoint returns 429 once the per-IP
// budget is exhausted. All httptest requests share one RemoteAddr, so they all
// count against the same key.
func TestLoginRateLimited(t *testing.T) {
	srv := newTestServer(t) // no owner; every login is invalid-creds (401) until throttled

	saw429 := false
	for i := 0; i < 15; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"x","password":"whatever1"}`))
		srv.Handler().ServeHTTP(rec, req)
		switch {
		case i < 10 && rec.Code != http.StatusUnauthorized:
			t.Fatalf("attempt %d = %d, want 401 before the limit", i, rec.Code)
		case rec.Code == http.StatusTooManyRequests:
			saw429 = true
		}
	}
	if !saw429 {
		t.Fatal("expected a 429 after exhausting the login rate limit")
	}
}
