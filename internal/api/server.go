package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/registry"
)

type Deps struct {
	Auth       *auth.Service
	Library    *registry.Registry
	Search     *registry.Registry
	Downloader *registry.Registry
	Dev        bool
}

type Server struct {
	deps   Deps
	router chi.Router
}

func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, router: chi.NewRouter()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() {
	s.router.Use(middleware.Recoverer)

	s.router.Route("/api/v1", func(r chi.Router) {
		// public
		r.Get("/health", s.handleHealth)
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup/admin", s.handleSetupAdmin)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		// protected
		r.Group(func(pr chi.Router) {
			pr.Use(s.requireAuth)
			pr.Get("/me", s.handleMe)
			pr.Get("/adapters/available", s.handleAdaptersAvailable)
		})
	})

	// SPA (embed.FS in prod, Vite proxy in --dev) — must be last.
	s.router.Handle("/*", s.spaHandler())
}
