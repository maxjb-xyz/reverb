package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/maxjb-xyz/reverb/internal/api"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/config"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/download/spotdl"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/library/subsonic"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search/spotify"
	"github.com/maxjb-xyz/reverb/internal/store"
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
	// Seed admin password from env if provided and not yet set.
	if cfg.AdminPassword != "" {
		if req, _ := authSvc.IsSetupRequired(context.Background()); req {
			_ = authSvc.SetAdminPassword(context.Background(), cfg.AdminPassword)
		}
	}
	if cfg.AuthDisabled {
		_ = authSvc.SetAuthDisabled(context.Background(), true)
		log.Printf("WARNING: auth is DISABLED (REVERB_AUTH_DISABLED) — all routes are unauthenticated; use only on a trusted LAN")
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

	// EventBus backs both the WS endpoint and the Manager's typed events.
	bus := events.New()

	dirty := &atomicDirty{}

	// The Builder constructs the active library/search/download services from the
	// current enabled adapter_instance rows. It is used for the initial build here
	// and reused by the API server to rebuild live on any adapter mutation.
	builder := wiring.NewBuilder(
		libraryReg, searchReg, downloaderReg,
		st.Q(), st, bus, download.RealClock{}, os.Getenv,
	)

	bundle, err := builder.Build(context.Background())
	if err != nil {
		log.Fatal(err)
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
	srv := api.NewServer(deps)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("reverb listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
