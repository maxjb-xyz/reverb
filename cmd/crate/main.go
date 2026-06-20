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
	"github.com/maximusjb/crate/internal/registry"
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

	srv := api.NewServer(api.Deps{
		Auth:       authSvc,
		Library:    registry.NewRegistry("library"),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Dev:        cfg.Dev,
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("crate listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
