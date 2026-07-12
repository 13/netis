package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/pihole"
	"netis/internal/proxmox"
	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web"
	"netis/internal/wireguard"
)

type integrationRunner map[string]func(context.Context) error

func (r integrationRunner) Run(ctx context.Context, name string) error {
	fn, ok := r[name]
	if !ok {
		return fmt.Errorf("%s not configured", name)
	}
	return fn(ctx)
}

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

	runNow := integrationRunner{}

	if pxURL, _ := st.GetSetting("proxmox_url"); pxURL != "" {
		tokenID, _ := st.GetSetting("proxmox_token_id")
		secret, _ := st.GetSetting("proxmox_secret")
		insecure, _ := st.GetSetting("proxmox_insecure")
		px := proxmox.NewSync(st, proxmox.NewClient(pxURL, tokenID, secret, insecure == "1"), evs)
		runNow["proxmox"] = func(ctx context.Context) error { _, err := px.RunOnce(ctx); return err }
		go px.Start(ctx, time.Minute)
	}

	if wgAddr, _ := st.GetSetting("wg_ssh_addr"); wgAddr != "" {
		wgUser, _ := st.GetSetting("wg_ssh_user")
		wgKey, _ := st.GetSetting("wg_ssh_key_path")
		wgIface, _ := st.GetSetting("wg_iface")
		if wgIface == "" {
			wgIface = "wg0"
		}
		if sshRunner, err := wireguard.NewSSHRunner(wgAddr, wgUser, wgKey); err != nil {
			log.Printf("wireguard ssh setup: %v", err)
		} else {
			wgSync := wireguard.NewSync(st, sshRunner, evs, wgIface)
			runNow["wireguard"] = func(ctx context.Context) error { _, err := wgSync.RunOnce(ctx); return err }
			go wgSync.Start(ctx, time.Minute)
		}
	}

	if phURL, _ := st.GetSetting("pihole_url"); phURL != "" {
		phPass, _ := st.GetSetting("pihole_password")
		phInsecure, _ := st.GetSetting("pihole_insecure")
		ph := pihole.NewSync(st, pihole.NewClient(phURL, phPass, phInsecure == "1"), evs)
		runNow["pihole"] = func(ctx context.Context) error { _, err := ph.RunOnce(ctx); return err }
		go ph.Start(ctx, time.Minute)
	}

	srv := web.NewServer(st, broker, sched, runNow)
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
