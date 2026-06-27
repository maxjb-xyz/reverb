package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/auth"
)

const sessionCookie = "reverb_session"

type ctxKey int

const userCtxKey ctxKey = iota

// currentUser returns the authenticated user injected by requireAuth.
func currentUser(r *http.Request) (auth.CurrentUser, bool) {
	cu, ok := r.Context().Value(userCtxKey).(auth.CurrentUser)
	return cu, ok
}

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
		cu, err := s.deps.Auth.ResolveSession(r.Context(), s.tokenFromRequest(r))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, cu)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireCapability gates a handler on the current user holding a capability.
// It must be mounted inside the requireAuth group so a CurrentUser is present.
func (s *Server) requireCapability(cap string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cu, ok := currentUser(r)
			if !ok || !cu.Has(cap) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return s.requireCapability(auth.CapAdmin)(next)
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
