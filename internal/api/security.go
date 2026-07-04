package api

import (
	"net/http"
	"net/url"
	"strings"
)

// contentSecurityPolicy is deliberately permissive where the app genuinely needs
// it and locked down everywhere else:
//   - script-src 'self': the built SPA loads a single same-origin module bundle;
//     there are no inline scripts, so no 'unsafe-inline' is granted to scripts.
//   - style-src 'unsafe-inline': the player's dynamic album-palette theming sets
//     inline style attributes at runtime, which requires this.
//   - img-src https:: album/artist art is frequently served straight from external
//     provider CDNs (e.g. Spotify), so remote https images must be allowed.
//   - connect-src includes ws/wss for the live-progress WebSocket.
//   - frame-ancestors 'none' + X-Frame-Options: DENY: this app is never framed.
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' data: https:; " +
	"media-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'; " +
	"font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; " +
	"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

// securityHeaders sets defensive response headers on every response (SPA + API).
// These are safe defaults for a same-origin single-page app served over a trusted
// reverse proxy; see contentSecurityPolicy for the CSP rationale.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// isStateChanging reports whether the method mutates server state (and therefore
// warrants CSRF scrutiny). Safe/idempotent methods are exempt.
func isStateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

// csrfGuard rejects state-changing requests whose Origin (or, absent that,
// Referer) names a host other than the one the request targeted. It is
// defense-in-depth on top of the session cookie's SameSite=Lax attribute: a
// browser always attaches Origin to a cross-site POST/PUT/DELETE, so a forged
// request is caught here even if a future browser relaxes SameSite semantics.
//
// Requests that carry neither header (curl, native apps, server-to-server) are
// allowed through: those clients cannot be driven by a malicious web page, which
// is the only threat CSRF describes. Dev mode is skipped because the Vite dev
// server issues cross-origin XHRs during local development.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Dev || !isStateChanging(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		src := r.Header.Get("Origin")
		if src == "" {
			src = r.Header.Get("Referer")
		}
		if src != "" && !sameHost(src, r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request blocked"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameHost reports whether rawURL's host (host:port) equals the request Host.
func sameHost(rawURL, host string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}
