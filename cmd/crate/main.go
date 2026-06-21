package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/maxjb-xyz/crate/internal/api"
	"github.com/maxjb-xyz/crate/internal/auth"
	"github.com/maxjb-xyz/crate/internal/config"
	"github.com/maxjb-xyz/crate/internal/download"
	"github.com/maxjb-xyz/crate/internal/download/spotdl"
	"github.com/maxjb-xyz/crate/internal/events"
	"github.com/maxjb-xyz/crate/internal/library/subsonic"
	"github.com/maxjb-xyz/crate/internal/matching"
	"github.com/maxjb-xyz/crate/internal/registry"
	"github.com/maxjb-xyz/crate/internal/search"
	"github.com/maxjb-xyz/crate/internal/search/spotify"
	"github.com/maxjb-xyz/crate/internal/store"
)

func main() {
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
		log.Printf("WARNING: auth is DISABLED (CRATE_AUTH_DISABLED) — all routes are unauthenticated; use only on a trusted LAN")
	}

	// Registries (explicit registration at the composition root — no init() side-effects).
	libraryReg := registry.NewRegistry("library")
	libraryReg.Register("subsonic", func() registry.Plugin { return subsonic.New() })
	searchReg := registry.NewRegistry("search")
	searchReg.Register("spotify", func() registry.Plugin { return spotify.New() })
	downloaderReg := registry.NewRegistry("downloader")
	downloaderReg.Register("spotdl", func() registry.Plugin { return spotdl.New() })

	// Build the active library adapter from the enabled adapter_instance row.
	instances, err := st.Q().ListAdapterInstances(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	libAdapter, err := buildLibraryAdapter(context.Background(), libraryReg, instances, os.Getenv)
	if err != nil {
		libAdapter = nil
		log.Printf("WARNING: library adapter not available: %v", err)
	} else if libAdapter == nil {
		log.Printf("no library adapter configured (add one via settings)")
	} else {
		log.Printf("library adapter active: %s", libAdapter.Name())
	}

	// Build active search sources + the matching service + the aggregator.
	sources := buildSearchSources(searchReg, instances, os.Getenv)
	var aggregator *search.Aggregator
	if len(sources) > 0 {
		var matcher search.Matcher
		if libAdapter != nil {
			matcher = matching.NewService(libAdapter, st.Q(), st.LibraryVersion)
		}
		aggregator = search.NewAggregator(sources, matcher, 8*time.Second)
		log.Printf("search sources active: %d", len(sources))
	} else {
		log.Printf("no search sources configured (add one via settings)")
	}

	// EventBus backs both the WS endpoint and the Manager's typed events.
	bus := events.New()

	dirty := &atomicDirty{}

	// Build the download Manager from enabled downloader instances.
	var manager *download.Manager
	downloaders := buildDownloaders(downloaderReg, instances, os.Getenv)
	if len(downloaders) > 0 && libAdapter != nil {
		var rematcher download.Rematcher
		rematcher = matching.NewService(libAdapter, st.Q(), st.LibraryVersion)
		manager = download.NewManager(
			download.Config{Workers: 2, DebounceWindow: 5 * time.Second},
			downloaders,
			download.NewSQLStore(st.Q()),
			bus,
			libAdapter,           // ScanController (StartScan/ScanStatus)
			rematcher,            // Rematcher
			st,                   // VersionBumper (LibraryVersion/SetLibraryVersion)
			download.RealClock{}, // production clock
		)
		manager.Start()
		defer manager.Stop()
		log.Printf("download manager active: %d downloader(s)", len(downloaders))
	} else if len(downloaders) > 0 {
		log.Printf("WARNING: downloaders configured but no library adapter — download manager disabled")
	} else {
		log.Printf("no downloaders configured (add one via settings)")
	}

	deps := api.Deps{
		Auth:        authSvc,
		Library:     libAdapter,
		Lib:         libraryReg,
		Search:      searchReg,
		Downloader:  downloaderReg,
		Adapters:    st.Q(),
		Events:      bus,
		ConfigDirty: dirty,
		Dev:         cfg.Dev,
	}
	if aggregator != nil {
		deps.SearchAggregator = aggregator
	}
	if manager != nil {
		deps.Downloads = manager
	}
	srv := api.NewServer(deps)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("crate listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
