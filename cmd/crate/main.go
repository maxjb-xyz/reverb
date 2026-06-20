package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/maximusjb/crate/internal/api"
	"github.com/maximusjb/crate/internal/config"
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
	srv := api.NewServer(api.Deps{})
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("crate listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
