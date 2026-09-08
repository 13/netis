package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"netis/internal/buildinfo"
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
			url, _ := st.GetSetting(ctx, "proxmox_url")
			if url == "" {
				return errNotConfigured
			}
			tokenID, _ := st.GetSetting(ctx, "proxmox_token_id")
			secret, _ := st.GetSetting(ctx, "proxmox_secret")
			insecure, _ := st.GetSetting(ctx, "proxmox_insecure")
			_, err := proxmox.NewSync(st, proxmox.NewClient(url, tokenID, secret, insecure == "1"), evs).RunOnce(ctx)
			return err
		},
		"pihole": func(ctx context.Context) error {
			url, _ := st.GetSetting(ctx, "pihole_url")
			if url == "" {
				return errNotConfigured
			}
			pass, _ := st.GetSetting(ctx, "pihole_password")
			insecure, _ := st.GetSetting(ctx, "pihole_insecure")
			_, err := pihole.NewSync(st, pihole.NewClient(url, pass, insecure == "1"), evs).RunOnce(ctx)
			return err
		},
		"wireguard": func(ctx context.Context) error {
			addr, _ := st.GetSetting(ctx, "wg_ssh_addr")
			if addr == "" {
				return errNotConfigured
			}
			user, _ := st.GetSetting(ctx, "wg_ssh_user")
			key, _ := st.GetSetting(ctx, "wg_ssh_key_path")
			iface, _ := st.GetSetting(ctx, "wg_iface")
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

// integrationNames is the fixed set of integrations the periodic sync drives.
var integrationNames = []string{"proxmox", "pihole", "wireguard"}

// startIntegrationSyncs periodically runs each integration from the current
// settings so changes take effect without a restart.
func startIntegrationSyncs(ctx context.Context, runNow integrationRunner, interval time.Duration) {
	for _, name := range integrationNames {
		go runIntegrationLoop(ctx, runNow, name, interval)
	}
}

// runIntegrationLoop runs one integration immediately, then every interval,
// reading current settings each time. An unconfigured integration
// (errNotConfigured) is skipped silently; other errors are logged.
func runIntegrationLoop(ctx context.Context, runNow integrationRunner, name string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := runNow.Run(ctx, name); err != nil && !errors.Is(err, errNotConfigured) {
			slog.Error("integration run failed", "name", name, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func main() {
	// A subcommand runs a one-shot job and exits; with no subcommand netis
	// starts the server.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate-db":
			if err := runMigrateDB(context.Background(), os.Args[2:]); err != nil {
				slog.Error("migrate-db", "err", err)
				os.Exit(1)
			}
			return
		case "healthcheck":
			if err := runHealthcheck(context.Background()); err != nil {
				slog.Error("healthcheck", "err", err)
				os.Exit(1)
			}
			return
		}
	}

	cfg := config.Load()
	st, err := store.Open(cfg.DSN)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	broker := events.NewBroker()
	evs := events.NewService(st, broker)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	offlineAfter := 3
	if v, _ := st.GetSetting(ctx, "offline_after"); v != "" {
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
	go sched.Start(ctx)

	runNow := newIntegrationRunner(st, evs)
	startIntegrationSyncs(ctx, runNow, time.Minute)
	startRetention(ctx, st, retentionInterval)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           web.NewServer(st, broker, sched, runNow).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// Request contexts derive from ctx so open SSE streams end on
		// SIGTERM and Shutdown can drain instead of hanging on them.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("netis listening", "addr", cfg.Addr, "version", buildinfo.Get().Label())

	select {
	case err := <-errCh:
		slog.Error("http server", "err", err)
		os.Exit(1)
	case <-ctx.Done():
	}
	slog.Info("netis shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("shutdown", "err", err)
		srv.Close()
	}
}
