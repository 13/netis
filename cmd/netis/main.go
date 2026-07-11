package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/proxmox"
	"netis/internal/scan"
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

	broker := events.NewBroker()
	evs := events.NewService(st, broker)

	offlineAfter := 3
	if v, _ := st.GetSetting("offline_after"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offlineAfter = n
		}
	}
	engine := &scan.Engine{
		Store: st, Events: evs, Broker: broker,
		Sweeper:      scan.NewICMPSweeper(64),
		ARP:          scan.ReadARPTable,
		Resolve:      scan.ResolveName,
		OfflineAfter: offlineAfter,
	}
	sched := scan.NewScheduler(engine, st)
	ctx := context.Background()
	go sched.Start(ctx)

	if pxURL, _ := st.GetSetting("proxmox_url"); pxURL != "" {
		tokenID, _ := st.GetSetting("proxmox_token_id")
		secret, _ := st.GetSetting("proxmox_secret")
		insecure, _ := st.GetSetting("proxmox_insecure")
		px := proxmox.NewSync(st, proxmox.NewClient(pxURL, tokenID, secret, insecure == "1"), evs)
		go px.Start(ctx, time.Minute)
	}

	srv := web.NewServer(st)
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
