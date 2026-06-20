package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/library"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
)

// Streamer is the subset of *search.Aggregator the SSE handler needs.
// *search.Aggregator satisfies it.
type Streamer interface {
	Stream(ctx context.Context, q string, t core.EntityType) <-chan search.Envelope
}

// DownloadManager is the subset of *download.Manager the API needs.
type DownloadManager interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
	List(ctx context.Context) ([]core.DownloadJob, error)
	Cancel(ctx context.Context, jobID string) error
	Retry(ctx context.Context, jobID string) (core.DownloadJob, error)
}

type Deps struct {
	Auth             *auth.Service
	Library          library.LibraryAdapter
	SearchAggregator Streamer
	Search           *registry.Registry
	Downloader       *registry.Registry
	Downloads        DownloadManager
	Dev              bool
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
		r.Get("/openapi.yaml", s.handleOpenAPI)

		// protected
		r.Group(func(pr chi.Router) {
			pr.Use(s.requireAuth)
			pr.Get("/me", s.handleMe)
			pr.Get("/adapters/available", s.handleAdaptersAvailable)
			pr.Get("/library/search", s.handleLibrarySearch)
			pr.Get("/library/artists", s.handleLibraryArtists)
			pr.Get("/library/artist/{id}", s.handleLibraryArtist)
			pr.Get("/library/album/{id}", s.handleLibraryAlbum)
			pr.Get("/library/albums", s.handleLibraryAlbums)
			pr.Get("/library/playlists", s.handleLibraryPlaylists)
			pr.Get("/stream/{id}", s.handleStream)
			pr.Get("/cover/{id}", s.handleCover)
			pr.Get("/search/everywhere", s.handleEverywhere)
			pr.Post("/downloads", s.handleCreateDownload)
			pr.Get("/downloads", s.handleListDownloads)
			pr.Post("/downloads/{id}/cancel", s.handleCancelDownload)
			pr.Post("/downloads/{id}/retry", s.handleRetryDownload)
		})
	})

	// SPA (embed.FS in prod, Vite proxy in --dev) — must be last.
	s.router.Handle("/*", s.spaHandler())
}
