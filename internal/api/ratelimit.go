package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a small fixed-window per-key limiter used to slow online
// password guessing on the auth endpoints. It is intentionally in-process and
// dependency-free: a self-hosted single instance does not need a shared store.
type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rlWindow
	max     int
	window  time.Duration
	now     func() time.Time
	lastGC  time.Time
}

type rlWindow struct {
	count int
	reset time.Time
}

func newRateLimiter(max int, window time.Duration, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{
		windows: make(map[string]*rlWindow),
		max:     max,
		window:  window,
		now:     now,
		lastGC:  now(),
	}
}

// allow records an attempt for key and reports whether it is within the limit.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	t := rl.now()
	rl.gc(t)
	w := rl.windows[key]
	if w == nil || t.After(w.reset) {
		rl.windows[key] = &rlWindow{count: 1, reset: t.Add(rl.window)}
		return true
	}
	if w.count >= rl.max {
		return false
	}
	w.count++
	return true
}

// gc drops expired windows so the map cannot grow without bound. It runs at most
// once per window duration (amortized O(1) per allow call).
func (rl *rateLimiter) gc(t time.Time) {
	if t.Sub(rl.lastGC) < rl.window {
		return
	}
	rl.lastGC = t
	for k, w := range rl.windows {
		if t.After(w.reset) {
			delete(rl.windows, k)
		}
	}
}

// rateLimitAuth throttles auth endpoints per client IP. When the limit is
// exceeded it returns 429 with a Retry-After hint.
func (s *Server) rateLimitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authLimiter != nil && !s.authLimiter.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts; please wait and try again"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the peer IP from RemoteAddr. It deliberately does NOT trust
// X-Forwarded-For: when Reverb is exposed directly, that header is attacker-
// controlled and would let an attacker rotate fake IPs to defeat the limiter.
// Behind a reverse proxy this keys on the proxy's address, which throttles the
// login surface as a whole — the right trade-off for a small single instance.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
