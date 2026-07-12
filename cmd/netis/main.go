package main

import (
	"context"
	"errors"
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

var errNotConfigured = errors.New("not configured")

// newIntegrationRunner builds a run-now registry whose closures read the current
// settings on each call, so "Run now" reflects settings saved after startup.
func newIntegrationRunner(st *store.Store, evs *events.Service) integrationRunner {
	return integrationRunner{
		"proxmox": func(ctx context.Context) error {
			url, _ := st.GetSetting("proxmox_url")
			if url == "" {
				return errNotConfigured
			}
			tokenID, _ := st.GetSetting("proxmox_token_id")
			secret, _ := st.GetSetting("proxmox_secret")
			insecure, _ := st.GetSetting("proxmox_insecure")
			_, err := proxmox.NewSync(st, proxmox.NewClient(url, tokenID, secret, insecure == "1"), evs).RunOnce(ctx)
			return err
		},
		"pihole": func(ctx context.Context) error {
			url, _ := st.GetSetting("pihole_url")
			if url == "" {
				return errNotConfigured
			}
			pass, _ := st.GetSetting("pihole_password")
			insecure, _ := st.GetSetting("pihole_insecure")
			_, err := pihole.NewSync(st, pihole.NewClient(url, pass, insecure == "1"), evs).RunOnce(ctx)
			return err
		},
		"wireguard": func(ctx context.Context) error {
			addr, _ := st.GetSetting("wg_ssh_addr")
			if addr == "" {
				return errNotConfigured
			}
			user, _ := st.GetSetting("wg_ssh_user")
			key, _ := st.GetSetting("wg_ssh_key_path")
			iface, _ := st.GetSetting("wg_iface")
			if iface == "" {
				iface = "wg0"
			}
			sshRunner, err := wireguard.NewSSHRunner(addr, user, key)
			if err != nil {
				return err
			}
			_, err = wireguard.NewSync(st, sshRunner, evs, iface).RunOnce(ctx)
			return err
		},
	}
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

	runNow := newIntegrationRunner(st, evs)

	if pxURL, _ := st.GetSetting("proxmox_url"); pxURL != "" {
		tokenID, _ := st.GetSetting("proxmox_token_id")
		secret, _ := st.GetSetting("proxmox_secret")
		insecure, _ := st.GetSetting("proxmox_insecure")
		go proxmox.NewSync(st, proxmox.NewClient(pxURL, tokenID, secret, insecure == "1"), evs).Start(ctx, time.Minute)
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
			go wireguard.NewSync(st, sshRunner, evs, wgIface).Start(ctx, time.Minute)
		}
	}

	if phURL, _ := st.GetSetting("pihole_url"); phURL != "" {
		phPass, _ := st.GetSetting("pihole_password")
		phInsecure, _ := st.GetSetting("pihole_insecure")
		go pihole.NewSync(st, pihole.NewClient(phURL, phPass, phInsecure == "1"), evs).Start(ctx, time.Minute)
	}

	srv := web.NewServer(st, broker, sched, runNow)
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
