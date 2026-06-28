package api

import (
	"context"
	"database/sql"
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
	"github.com/maxjb-xyz/reverb/internal/request"
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

// PlaylistOwnerStore is the persistence slice the playlist-ownership checks need.
// *db.Queries (from store.Store.Q()) satisfies it directly. It is intentionally
// separate from the playlistsync.Service (which the background scheduler also
// drives, and which must stay owner-agnostic): owner scoping lives ONLY in the
// API handlers. When nil, ownership scoping is disabled (legacy/test fallback).
type PlaylistOwnerStore interface {
	ListSyncedPlaylistsCountForOwner(ctx context.Context, ownerUserID sql.NullString) ([]db.ListSyncedPlaylistsCountForOwnerRow, error)
	GetSyncedPlaylistOwner(ctx context.Context, id string) (sql.NullString, error)
	SetSyncedPlaylistOwner(ctx context.Context, arg db.SetSyncedPlaylistOwnerParams) error
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
	// PlaylistOwner backs the playlist-ownership checks in the API handlers.
	// When nil, ownership scoping is disabled (handlers fall back to unscoped
	// behavior) — used by handler tests that authenticate as the admin owner.
	PlaylistOwner PlaylistOwnerStore
	ConfigDirty   ConfigDirty
	Reload        ServiceReloader
	Dev           bool
	Version       string
	// DataDir is the directory where Reverb persists app data (same dir as the
	// SQLite DB). Used by the playlist-cover upload handler. When empty, cover
	// uploads are unavailable.
	DataDir string
	// LibraryStatus reports (mode, state) for the bundled-library status endpoint.
	// nil in tests/legacy — handler falls back based on whether a library adapter is present.
	LibraryStatus func() (mode string, state string)
	// Requests is the request service. Nil in tests/legacy that don't use the request system.
	Requests *request.Service
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
		r.Post("/auth/signup", s.handleSignup)
		r.Get("/auth/registration-status", s.handleRegistrationStatus)
		r.Get("/openapi.yaml", s.handleOpenAPI)
		r.Get("/version", s.handleVersion)

		// protected
		r.Group(func(pr chi.Router) {
			pr.Use(s.requireAuth)
			pr.Get("/account", s.handleAccount)
			pr.Post("/account/password", s.handleChangePassword)
			pr.Post("/account/logout-all", s.handleLogoutAll)
			pr.Get("/me", s.handleMe)
			pr.Get("/config/pending-restart", s.handlePendingRestart)
			pr.Get("/library/status", s.handleLibraryStatus)
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
			// playlist READS stay on plain auth (ownership is enforced in-handler).
			pr.Get("/playlists", s.handleListSyncedPlaylists)
			pr.Get("/playlists/{id}", s.handleSyncedPlaylistDetail)
			pr.Get("/playlists/{id}/cover", s.handleServePlaylistCover)
			// download queue controls + reads stay on plain auth.
			pr.Post("/downloads/pause", s.handlePauseQueue)
			pr.Post("/downloads/resume", s.handleResumeQueue)
			pr.Get("/downloads/queue", s.handleQueueState)
			pr.Post("/downloads/clear", s.handleClearDownloads)
			pr.Post("/downloads/{id}/clear", s.handleClearDownload)
			pr.Get("/downloads", s.handleListDownloads)
			pr.Post("/downloads/{id}/cancel", s.handleCancelDownload)
			pr.Post("/downloads/{id}/retry", s.handleRetryDownload)
			pr.Get("/ws", s.handleWS)

			// manage library & integrations: adapter CRUD + server settings.
			pr.Group(func(mr chi.Router) {
				mr.Use(s.requireCapability(auth.CapManageLibrary))
				mr.Get("/adapters/available", s.handleAdaptersAvailable)
				mr.Get("/adapters", s.handleListAdapters)
				mr.Post("/adapters", s.handleCreateAdapter)
				mr.Put("/adapters/{id}", s.handleUpdateAdapter)
				mr.Delete("/adapters/{id}", s.handleDeleteAdapter)
				mr.Post("/adapters/test", s.handleTestAdapter)
				mr.Get("/settings", s.handleGetSettings)
				mr.Put("/settings", s.handlePutSettings)
			})

			// download tracks: enqueue create + batch.
			pr.Group(func(dr chi.Router) {
				dr.Use(s.requireCapability(auth.CapAutoApprove))
				dr.Post("/downloads/batch", s.handleBatchDownload)
				dr.Post("/downloads", s.handleCreateDownload)
			})

			// create playlists: every playlist WRITE (create/import/mutate).
			pr.Group(func(cr chi.Router) {
				cr.Use(s.requireCapability(auth.CapCreatePlaylists))
				cr.Post("/playlists/import", s.handleImportPlaylistOnce)
				cr.Post("/playlists/import-synced", s.handleImportSyncedPlaylist)
				cr.Post("/playlists", s.handleCreatePlaylist)
				cr.Put("/playlists/{id}", s.handleRenameSyncedPlaylist)
				cr.Post("/playlists/{id}/sync", s.handleSyncNow)
				cr.Post("/playlists/{id}/download-missing", s.handleSyncedDownloadMissing)
				cr.Put("/playlists/{id}/settings", s.handleSyncedSettings)
				cr.Delete("/playlists/{id}", s.handleDeleteSyncedPlaylist)
				cr.Post("/playlists/{id}/tracks", s.handleAddSyncedTrack)
				cr.Delete("/playlists/{id}/tracks", s.handleRemoveSyncedTrack)
				cr.Post("/playlists/{id}/cover", s.handleUploadPlaylistCover)
				cr.Put("/playlists/{id}/tracks/order", s.handleReorderSyncedTracks)
			})

			// request music: create/list own/cancel.
			pr.Group(func(rr chi.Router) {
				rr.Use(s.requireCapability(auth.CapRequest))
				rr.Post("/requests", s.handleCreateRequest)
				rr.Get("/requests/mine", s.handleListMyRequests)
				rr.Post("/requests/{id}/cancel", s.handleCancelRequest)
			})

			// manage requests: list all + approve/deny.
			pr.Group(func(mr chi.Router) {
				mr.Use(s.requireCapability(auth.CapManageRequests))
				mr.Get("/requests", s.handleListRequests)
				mr.Post("/requests/{id}/approve", s.handleApproveRequest)
				mr.Post("/requests/{id}/deny", s.handleDenyRequest)
			})

			// admin-only: user management
			pr.Group(func(ar chi.Router) {
				ar.Use(s.requireCapability(auth.CapManageUsers))
				ar.Get("/users", s.handleListUsers)
				ar.Post("/users", s.handleCreateUser)
				ar.Patch("/users/{id}", s.handleUpdateUser)
				ar.Delete("/users/{id}", s.handleDeleteUser)
				ar.Post("/users/{id}/password", s.handleAdminResetPassword)
			})

			// admin-only: role management + capability registry
			pr.Group(func(ar chi.Router) {
				ar.Use(s.requireAdmin)
				ar.Get("/roles", s.handleListRoles)
				ar.Post("/roles", s.handleCreateRole)
				ar.Patch("/roles/{id}", s.handleUpdateRole)
				ar.Delete("/roles/{id}", s.handleDeleteRole)
				ar.Get("/capabilities", s.handleCapabilities)
				// registration policy + invites
				ar.Get("/settings/registration", s.handleGetRegistration)
				ar.Patch("/settings/registration", s.handlePatchRegistration)
				ar.Get("/invites", s.handleListInvites)
				ar.Post("/invites", s.handleCreateInvite)
				ar.Delete("/invites/{id}", s.handleDeleteInvite)
			})
		})
	})

	// SPA (embed.FS in prod, Vite proxy in --dev) — must be last.
	s.router.Handle("/*", s.spaHandler())
}
