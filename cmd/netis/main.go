package main

import (
	"log"
	"net/http"

	"netis/internal/config"
	"netis/internal/store"
	"netis/internal/web"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	srv := web.NewServer(st)
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
