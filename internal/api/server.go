package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Streamer is the subset of *search.Aggregator the SSE handler needs.
// *search.Aggregator satisfies it.
type Streamer interface {
	Stream(ctx context.Context, q string, t core.EntityType) <-chan search.Envelope
}

// EventSubscriber is the EventBus slice the WS handler needs.
type EventSubscriber interface {
	Subscribe(topic string) (<-chan events.Event, func())
}

// DownloadManager is the subset of *download.Manager the API needs.
type DownloadManager interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
	List(ctx context.Context) ([]core.DownloadJob, error)
	Cancel(ctx context.Context, jobID string) error
	Retry(ctx context.Context, jobID string) (core.DownloadJob, error)
}

// AdapterStore is the persistence slice the adapter + settings handlers need.
// *db.Queries (from store.Store.Q()) satisfies it directly.
type AdapterStore interface {
	ListAdapterInstances(ctx context.Context) ([]db.AdapterInstance, error)
	GetAdapterInstance(ctx context.Context, id string) (db.AdapterInstance, error)
	CreateAdapterInstance(ctx context.Context, arg db.CreateAdapterInstanceParams) error
	UpdateAdapterInstance(ctx context.Context, arg db.UpdateAdapterInstanceParams) error
	DeleteAdapterInstance(ctx context.Context, id string) error
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// ConfigDirty tracks whether adapter/settings config changed since startup. The
// restart-to-apply UX reads this so it can show a "Restart to apply" banner.
type ConfigDirty interface {
	Set()
	Dirty() bool
}

type Deps struct {
	Auth             *auth.Service
	Library          library.LibraryAdapter
	SearchAggregator Streamer
	Search           *registry.Registry
	Downloader       *registry.Registry
	Lib              *registry.Registry
	Downloads        DownloadManager
	Events           EventSubscriber
	Adapters         AdapterStore
	ConfigDirty      ConfigDirty
	Dev              bool
	Version          string
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
		r.Get("/version", s.handleVersion)

		// protected
		r.Group(func(pr chi.Router) {
			pr.Use(s.requireAuth)
			pr.Get("/me", s.handleMe)
			pr.Get("/adapters/available", s.handleAdaptersAvailable)
			pr.Get("/adapters", s.handleListAdapters)
			pr.Post("/adapters", s.handleCreateAdapter)
			pr.Put("/adapters/{id}", s.handleUpdateAdapter)
			pr.Delete("/adapters/{id}", s.handleDeleteAdapter)
			pr.Post("/adapters/test", s.handleTestAdapter)
			pr.Get("/settings", s.handleGetSettings)
			pr.Put("/settings", s.handlePutSettings)
			pr.Get("/config/pending-restart", s.handlePendingRestart)
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
			pr.Get("/ws", s.handleWS)
		})
	})

	// SPA (embed.FS in prod, Vite proxy in --dev) — must be last.
	s.router.Handle("/*", s.spaHandler())
}
