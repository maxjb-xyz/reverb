package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"

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

// DownloadManager is the subset of *download.Manager the API needs. Stop is used
// by the live-reload path to shut down the previous Manager after a new one has
// been swapped in.
type DownloadManager interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
	List(ctx context.Context) ([]core.DownloadJob, error)
	Cancel(ctx context.Context, jobID string) error
	Retry(ctx context.Context, jobID string, manualURL string) (core.DownloadJob, error)
	Pause()
	Resume()
	IsPaused() bool
	Clear(ctx context.Context, jobID string) error
	ClearFinished(ctx context.Context) ([]string, error)
	Stop()
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

// ConfigDirty tracks whether settings config changed since startup. Adapter
// mutations now apply live (no restart), so this is retained only for any
// non-adapter settings flow; the adapter handlers never set it.
type ConfigDirty interface {
	Set()
	Dirty() bool
}

// ServiceReloader rebuilds the active library/search/download/coverage/sync services
// from the current DB state and returns them. The returned Manager (if any) is
// already Started; the server Stops the previous one after swapping the new one in.
// A nil interface result means "no service of that kind is configured".
type ServiceReloader interface {
	Reload(ctx context.Context) (lib library.LibraryAdapter, search Streamer, coverage CoverageService, downloads DownloadManager, sync SyncService, err error)
}

type Deps struct {
	Auth             *auth.Service
	Library          library.LibraryAdapter
	SearchAggregator Streamer
	Coverage         CoverageService
	Search           *registry.Registry
	Downloader       *registry.Registry
	Lib              *registry.Registry
	Downloads        DownloadManager
	Sync             SyncService
	Events           EventSubscriber
	Adapters         AdapterStore
	ConfigDirty      ConfigDirty
	Reload           ServiceReloader
	Dev              bool
	Version          string
	// DataDir is the directory where Reverb persists app data (same dir as the
	// SQLite DB). Used by the playlist-cover upload handler. When empty, cover
	// uploads are unavailable.
	DataDir string
}

type Server struct {
	deps   Deps
	router chi.Router

	// live holds the currently active services. Handlers read them through the
	// getters under the RLock; reload swaps them under the write lock so adapter
	// mutations take effect without a restart.
	mu   sync.RWMutex
	live struct {
		library   library.LibraryAdapter
		search    Streamer
		coverage  CoverageService
		downloads DownloadManager
		// sync is reload-swapped alongside coverage: when the Spotify adapter or
		// library changes, the new SyncService (or nil) is atomically installed
		// without a restart.
		sync SyncService
	}
}

func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, router: chi.NewRouter()}
	s.live.library = deps.Library
	s.live.search = deps.SearchAggregator
	s.live.coverage = deps.Coverage
	s.live.downloads = deps.Downloads
	s.live.sync = deps.Sync
	// Ensure the playlist-covers directory exists when a data dir is configured.
	if deps.DataDir != "" {
		_ = os.MkdirAll(filepath.Join(deps.DataDir, "playlist-covers"), 0o755)
	}
	s.routes()
	return s
}

// library / searchAggregator / downloads return the currently active service
// under the read lock. Any may be nil when nothing of that kind is configured.
func (s *Server) library() library.LibraryAdapter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.library
}

func (s *Server) searchAggregator() Streamer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.search
}

func (s *Server) downloads() DownloadManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.downloads
}

// reload rebuilds the active services from the current DB state and atomically
// swaps them in. The previous download Manager is Stopped after the swap so
// in-flight reads never see a stopped Manager. A no-op when no reloader is wired.
func (s *Server) reload(ctx context.Context) error {
	if s.deps.Reload == nil {
		return nil
	}
	lib, srch, cov, dl, snc, err := s.deps.Reload.Reload(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.live.downloads
	s.live.library, s.live.search, s.live.coverage, s.live.downloads, s.live.sync = lib, srch, cov, dl, snc
	s.mu.Unlock()
	// Stop the previous Manager only after the new one is swapped in, and never
	// stop a nil or unchanged Manager.
	if old != nil && old != dl {
		old.Stop()
	}
	return nil
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
			pr.Get("/stream/{id}", s.handleStream)
			pr.Get("/cover/{id}", s.handleCover)
			pr.Get("/search/everywhere", s.handleEverywhere)
			pr.Get("/artist/{source}/{id}", s.handleArtistDetail)
			pr.Get("/artist/{source}/{id}/profile", s.handleArtistProfile)
			pr.Get("/artist/{source}/{id}/coverage", s.handleArtistCoverage)
			pr.Get("/album/{source}/{id}", s.handleAlbumDetail)
			pr.Post("/playlists/import", s.handleImportPlaylistOnce)
			pr.Post("/playlists/import-synced", s.handleImportSyncedPlaylist)
			pr.Get("/playlists", s.handleListSyncedPlaylists)
			pr.Post("/playlists", s.handleCreatePlaylist)
			pr.Get("/playlists/{id}", s.handleSyncedPlaylistDetail)
			pr.Put("/playlists/{id}", s.handleRenameSyncedPlaylist)
			pr.Post("/playlists/{id}/sync", s.handleSyncNow)
			pr.Post("/playlists/{id}/download-missing", s.handleSyncedDownloadMissing)
			pr.Put("/playlists/{id}/settings", s.handleSyncedSettings)
			pr.Delete("/playlists/{id}", s.handleDeleteSyncedPlaylist)
			pr.Post("/playlists/{id}/tracks", s.handleAddSyncedTrack)
			pr.Delete("/playlists/{id}/tracks", s.handleRemoveSyncedTrack)
			pr.Post("/playlists/{id}/cover", s.handleUploadPlaylistCover)
			pr.Get("/playlists/{id}/cover", s.handleServePlaylistCover)
			pr.Put("/playlists/{id}/tracks/order", s.handleReorderSyncedTracks)
			pr.Post("/downloads/pause", s.handlePauseQueue)
			pr.Post("/downloads/resume", s.handleResumeQueue)
			pr.Get("/downloads/queue", s.handleQueueState)
			pr.Post("/downloads/clear", s.handleClearDownloads)
			pr.Post("/downloads/{id}/clear", s.handleClearDownload)
			pr.Post("/downloads/batch", s.handleBatchDownload)
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
