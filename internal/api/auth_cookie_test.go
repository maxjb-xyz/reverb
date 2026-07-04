package api

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sessionCookieFromSetup completes first-run setup on a fresh server and returns
// the session cookie it set, after applying mutate to the setup request.
func sessionCookieFromSetup(t *testing.T, mutate func(*http.Request)) *http.Cookie {
	t.Helper()
	srv := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"username":"owner","password":"pw123456"}`))
	if mutate != nil {
		mutate(req)
	}
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup/admin = %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func TestSessionCookieInsecureOverPlainHTTP(t *testing.T) {
	c := sessionCookieFromSetup(t, nil)
	if c.Secure {
		t.Fatal("session cookie must NOT be Secure over plain http (the browser drops it on a LAN, causing the login loop)")
	}
	if !c.HttpOnly {
		t.Fatal("session cookie must stay HttpOnly")
	}
}

func TestSessionCookieSecureBehindHTTPSProxy(t *testing.T) {
	c := sessionCookieFromSetup(t, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") })
	if !c.Secure {
		t.Fatal("session cookie must be Secure when X-Forwarded-Proto=https")
	}
}

func TestSessionCookieSecureOverDirectTLS(t *testing.T) {
	c := sessionCookieFromSetup(t, func(r *http.Request) { r.TLS = &tls.ConnectionState{} })
	if !c.Secure {
		t.Fatal("session cookie must be Secure over a direct TLS connection")
	}
}
