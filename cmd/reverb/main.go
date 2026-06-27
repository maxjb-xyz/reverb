package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/maxjb-xyz/reverb/internal/api"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/config"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/download/lidarr"
	"github.com/maxjb-xyz/reverb/internal/download/spotdl"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/library/embedded"
	"github.com/maxjb-xyz/reverb/internal/library/subsonic"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search/spotify"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

func main() {
	log.Printf("reverb %s starting", version)

	// Root context cancelled when main returns, so background goroutines (e.g. the
	// playlist-sync scheduler) shut down with the process.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatal(err)
	}

	authSvc := auth.NewService(st.Q(), time.Now)
	// Bootstrap an owner from REVERB_ADMIN_PASSWORD when no users exist yet: stash
	// the hash in the legacy admin_password_hash setting so EnsureSeed below migrates
	// it into the "admin" owner account (single, idempotent code path).
	if cfg.AdminPassword != "" {
		if req, _ := authSvc.IsSetupRequired(context.Background()); req {
			if h, err := auth.HashPassword(cfg.AdminPassword); err == nil {
				_ = st.Q().UpsertSetting(context.Background(), db.UpsertSettingParams{Key: "admin_password_hash", Value: h})
			}
		}
	}
	if cfg.AuthDisabled {
		log.Printf("WARNING: REVERB_AUTH_DISABLED is no longer supported — auth is always enforced; ignoring")
	}
	// Seed system roles + registration-policy defaults, and migrate a legacy
	// single-admin install into an owner account. Idempotent; fail loudly on error.
	if err := authSvc.EnsureSeed(context.Background()); err != nil {
		log.Fatalf("seed identity defaults: %v", err)
	}

	// spotDL is bundled with the image, so present it as a configured downloader
	// out of the box (no manual setup) when none exists yet.
	seedBundledDownloader(context.Background(), st.Q(), os.Getenv)

	// Registries (explicit registration at the composition root — no init() side-effects).
	libraryReg := registry.NewRegistry("library")
	libraryReg.Register("subsonic", func() registry.Plugin { return subsonic.New() })
	searchReg := registry.NewRegistry("search")
	searchReg.Register("spotify", func() registry.Plugin { return spotify.New() })
	downloaderReg := registry.NewRegistry("downloader")
	downloaderReg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	downloaderReg.Register("lidarr", func() registry.Plugin { return lidarr.New() })
	// Surface the async capability to the admin UI (/adapters/available).
	registry.RegisterCapability("async", func(p registry.Plugin) bool {
		_, ok := p.(download.AsyncDownloader)
		return ok
	})

	// EventBus backs both the WS endpoint and the Manager's typed events.
	bus := events.New()

	dirty := &atomicDirty{}

	// The Builder constructs the active library/search/download services from the
	// current enabled adapter_instance rows. It is used for the initial build here
	// and reused by the API server to rebuild live on any adapter mutation.
	builder := wiring.NewBuilder(
		libraryReg, searchReg, downloaderReg,
		st.Q(), st, bus, download.RealClock{}, os.Getenv,
		filepath.Dir(cfg.DBPath),
	)

	bundle, err := builder.Build(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	// Start the bundled-library supervisor (no-op in external mode).
	if bundle.Supervisor != nil {
		bundle.Supervisor.Start()
	}

	if bundle.Manager != nil {
		bundle.Manager.Start()
		defer bundle.Manager.Stop()
	}

	// Start the playlist-sync scheduler when a sync service is configured. It ticks
	// every 15 minutes, syncing due playlists, and stops when ctx is cancelled.
	if bundle.Sync != nil {
		go playlistsync.NewScheduler(bundle.Sync, 15*time.Minute).Run(ctx)
		// One-time migration: copy existing Navidrome playlists into managed playlists.
		// Runs in the background so startup is not blocked; guarded by a settings flag.
		go func() {
			if err := bundle.Sync.MigrateLibraryPlaylists(ctx); err != nil {
				log.Printf("WARNING: library playlist migration: %v", err)
			}
		}()
	}

	deps := api.Deps{
		Auth:        authSvc,
		Library:     bundle.Library,
		Lib:         libraryReg,
		Search:      searchReg,
		Downloader:  downloaderReg,
		Adapters:    st.Q(),
		Events:      bus,
		ConfigDirty: dirty,
		Reload:      &serviceReloader{builder: builder},
		Dev:         cfg.Dev,
		Version:     version,
		DataDir:     filepath.Dir(cfg.DBPath),
	}
	// Guard against the "non-nil interface wrapping a nil pointer" trap: only set
	// the interface fields when the concrete service is actually present.
	if bundle.Aggregator != nil {
		deps.SearchAggregator = bundle.Aggregator
	}
	if bundle.Coverage != nil {
		deps.Coverage = bundle.Coverage
	}
	if bundle.Manager != nil {
		deps.Downloads = bundle.Manager
	}
	if bundle.Sync != nil {
		deps.Sync = bundle.Sync
	}
	if bundle.Supervisor != nil {
		sup := bundle.Supervisor
		// LibraryStatus closure and supervisor are boot-bound: backend-mode changes are
		// restart-only, so bundle is immutable after wiring. The unsynchronised
		// bundle.Library read below is safe — the bundle is never mutated post-boot.
		deps.LibraryStatus = func() (string, string) {
			h := sup.Health()
			if h == embedded.HealthExternal {
				if bundle.Library != nil {
					return "external", "ready"
				}
				return "external", "unconfigured"
			}
			return "built-in", string(h)
		}
	}
	srv := api.NewServer(deps)

	addr := fmt.Sprintf(":%d", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("reverb listening on %s (dev=%v)", addr, cfg.Dev)

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; close(stop) }()

	httpSrv := &http.Server{Handler: srv.Handler()}
	if err := serveWithShutdown(httpSrv, ln, stop, func(ctx context.Context) error {
		if bundle.Supervisor != nil {
			return bundle.Supervisor.Shutdown(ctx)
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}
