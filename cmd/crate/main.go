package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/maximusjb/crate/internal/api"
	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/config"
	"github.com/maximusjb/crate/internal/library/subsonic"
	"github.com/maximusjb/crate/internal/matching"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/search/spotify"
	"github.com/maximusjb/crate/internal/store"
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
	sources, err := buildSearchSources(searchReg, instances, os.Getenv)
	if err != nil {
		log.Printf("WARNING: search sources not available: %v", err)
		sources = nil
	}
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

	deps := api.Deps{
		Auth:       authSvc,
		Library:    libAdapter,
		Search:     searchReg,
		Downloader: downloaderReg,
		Dev:        cfg.Dev,
	}
	if aggregator != nil {
		deps.SearchAggregator = aggregator
	}
	srv := api.NewServer(deps)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("crate listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
