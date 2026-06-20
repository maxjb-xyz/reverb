package main

import (
	"log"
	"net/http"

	"github.com/maximusjb/crate/internal/api"
)

func main() {
	srv := api.NewServer(api.Deps{})
	addr := ":8090"
	log.Printf("crate listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
