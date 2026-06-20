package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// spaHandler serves the frontend. In dev it proxies to Vite; in prod it serves
// the embedded build (wired in Task 11 via setEmbeddedFS).
func (s *Server) spaHandler() http.Handler {
	if s.deps.Dev {
		target, _ := url.Parse("http://localhost:5173")
		return httputil.NewSingleHostReverseProxy(target)
	}
	return s.embeddedSPA()
}
