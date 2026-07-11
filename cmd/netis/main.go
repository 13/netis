package main

import (
	"log"
	"net/http"

	"netis/internal/config"
	"netis/internal/web"
)

func main() {
	cfg := config.Load()
	srv := web.NewServer()
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
