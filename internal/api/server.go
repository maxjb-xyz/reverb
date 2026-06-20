package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Deps holds the server's dependencies. It grows across M0 tasks.
type Deps struct{}

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
	s.router.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
	})
}
