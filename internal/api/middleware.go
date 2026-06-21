package api

import (
	"net/http"
	"strings"
)

const sessionCookie = "reverb_session"

func (s *Server) tokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	const p = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if disabled, _ := s.deps.Auth.IsAuthDisabled(r.Context()); disabled {
			next.ServeHTTP(w, r)
			return
		}
		ok, _ := s.deps.Auth.ValidateToken(r.Context(), s.tokenFromRequest(r))
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cookieSecure reports whether the session cookie should carry the Secure flag,
// based on the real request scheme. Direct TLS or an https-terminating reverse
// proxy (X-Forwarded-Proto: https) → Secure. Plain http (LAN, no TLS) → not Secure,
// otherwise the browser silently drops the cookie and every authed request 401s.
func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	maxAge := 30 * 24 * 3600
	if token == "" {
		maxAge = -1 // delete the cookie
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   maxAge,
	})
}
