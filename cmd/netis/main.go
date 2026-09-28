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
	"sync"
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

// integrationRunTimeout bounds one integration run, so a hung remote host
// (an SSH session that never answers, an API that never responds) cannot
// stall that integration's loop or hold its lock forever.
const integrationRunTimeout = 30 * time.Second

// integrationFunc runs one integration from the current settings and returns
// the item count and detail line for its status row.
type integrationFunc func(context.Context) (int, string, error)

// integration is one named integration and its run state. mu is held for the
// length of a run, so the periodic loop and Run now never overlap; failing is
// guarded by it and remembers an ongoing outage so it is announced once.
type integration struct {
	run     integrationFunc
	mu      sync.Mutex
	failing bool
}

// integrationRunner is the single path every integration run takes, periodic
// or Run now: it serialises runs per integration, applies the per-run
// deadline, and records the outcome as the integration's status.
type integrationRunner struct {
	st      *store.Store
	evs     *events.Service
	timeout time.Duration
	byName  map[string]*integration
}

func newRunner(st *store.Store, evs *events.Service, timeout time.Duration, funcs map[string]integrationFunc) *integrationRunner {
	r := &integrationRunner{st: st, evs: evs, timeout: timeout, byName: make(map[string]*integration, len(funcs))}
	for name, fn := range funcs {
		r.byName[name] = &integration{run: fn}
	}
	return r
}

// Run runs the named integration once and records its status. It returns
// web.ErrIntegrationBusy without waiting if that integration is already
// running, and errNotConfigured — recording nothing — if it has no settings.
func (r *integrationRunner) Run(ctx context.Context, name string) error {
	in, ok := r.byName[name]
	if !ok {
		return fmt.Errorf("%s not configured", name)
	}
	if !in.mu.TryLock() {
		return web.ErrIntegrationBusy
	}
	defer in.mu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	count, detail, err := in.run(runCtx)
	cancel()
	if errors.Is(err, errNotConfigured) {
		return err
	}
	// The run's context may have expired or been cancelled by shutdown; the
	// status write gets its own short deadline so the outcome is still kept.
	recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	r.recordStatus(recCtx, name, in, count, detail, err)
	return err
}

// recordStatus writes the integration_status row and raises one scan_error
// event per outage. The caller holds in.mu.
func (r *integrationRunner) recordStatus(ctx context.Context, name string, in *integration, count int, detail string, err error) {
	st := store.IntegrationStatus{Name: name, LastRun: time.Now().UTC().Format(time.RFC3339)}
	if err != nil {
		if !in.failing {
			in.failing = true
			r.evs.Emit(ctx, "scan_error", nil, name+" sync failing: "+err.Error())
		}
		st.Detail = err.Error()
	} else {
		in.failing = false
		st.OK = true
		st.ItemCount = count
		st.Detail = detail
	}
	if serr := r.st.SetIntegrationStatus(ctx, st); serr != nil {
		slog.Error("integration status write", "name", name, "err", serr)
	}
	r.evs.Broker().Publish("dashboard", "refresh")
}

var errNotConfigured = errors.New("not configured")

// readSettings reads several settings at once, failing on the first error
// rather than treating it as an empty value. A credential that cannot be
// decrypted — the wrong NETIS_SECRET_KEY, say — would otherwise read as blank
// and the integration would report an authentication failure against the
// remote host instead of the local misconfiguration it actually is.
func readSettings(ctx context.Context, st *store.Store, keys ...string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v, err := st.GetSetting(ctx, k)
		if err != nil {
			return nil, fmt.Errorf("reading setting %s: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// newIntegrationRunner builds the runner for the real integrations. Each
// closure reads the current settings on every call, so both the periodic sync
// and "Run now" pick up settings saved after startup.
func newIntegrationRunner(st *store.Store, evs *events.Service) *integrationRunner {
	return newRunner(st, evs, integrationRunTimeout, map[string]integrationFunc{
		"proxmox": func(ctx context.Context) (int, string, error) {
			s, err := readSettings(ctx, st, "proxmox_url", "proxmox_token_id", "proxmox_secret", "proxmox_insecure")
			if err != nil {
				return 0, "", err
			}
			if s["proxmox_url"] == "" {
				return 0, "", errNotConfigured
			}
			client := proxmox.NewClient(s["proxmox_url"], s["proxmox_token_id"],
				s["proxmox_secret"], s["proxmox_insecure"] == "1")
			stats, err := proxmox.NewSync(st, client, evs).RunOnce(ctx)
			if err != nil {
				return 0, "", err
			}
			count, detail := stats.Status()
			return count, detail, nil
		},
		"pihole": func(ctx context.Context) (int, string, error) {
			s, err := readSettings(ctx, st, "pihole_url", "pihole_password", "pihole_insecure")
			if err != nil {
				return 0, "", err
			}
			if s["pihole_url"] == "" {
				return 0, "", errNotConfigured
			}
			client := pihole.NewClient(s["pihole_url"], s["pihole_password"], s["pihole_insecure"] == "1")
			stats, err := pihole.NewSync(st, client, evs).RunOnce(ctx)
			if err != nil {
				return 0, "", err
			}
			count, detail := stats.Status()
			return count, detail, nil
		},
		"wireguard": func(ctx context.Context) (int, string, error) {
			s, err := readSettings(ctx, st, "wg_ssh_addr", "wg_ssh_user", "wg_ssh_key_path",
				"wg_ssh_known_hosts", "wg_iface")
			if err != nil {
				return 0, "", err
			}
			addr, user, key := s["wg_ssh_addr"], s["wg_ssh_user"], s["wg_ssh_key_path"]
			knownHosts, iface := s["wg_ssh_known_hosts"], s["wg_iface"]
			if addr == "" {
				return 0, "", errNotConfigured
			}
			if iface == "" {
				iface = "wg0"
			}
			sshRunner, err := wireguard.NewSSHRunner(addr, user, key, knownHosts)
			if err != nil {
				return 0, "", err
			}
			stats, err := wireguard.NewSync(st, sshRunner, evs, iface).RunOnce(ctx)
			if err != nil {
				return 0, "", err
			}
			count, detail := stats.Status()
			return count, detail, nil
		},
	})
}

// integrationNames is the fixed set of integrations the periodic sync drives.
var integrationNames = []string{"proxmox", "pihole", "wireguard"}

// startIntegrationSyncs periodically runs each integration from the current
// settings so changes take effect without a restart. Each loop is tracked on
// wg so shutdown can wait for an in-flight run before closing the store.
func startIntegrationSyncs(ctx context.Context, wg *sync.WaitGroup, runNow *integrationRunner, interval time.Duration) {
	for _, name := range integrationNames {
		wg.Go(func() { runIntegrationLoop(ctx, runNow, name, interval) })
	}
}

// runIntegrationLoop runs one integration immediately, then every interval,
// reading current settings each time. An unconfigured integration
// (errNotConfigured) is skipped silently, as is a tick that finds a Run now
// still in progress; other errors are logged.
func runIntegrationLoop(ctx context.Context, runNow *integrationRunner, name string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := runNow.Run(ctx, name); err != nil && !errors.Is(err, errNotConfigured) &&
			!errors.Is(err, web.ErrIntegrationBusy) {
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
		case "backup":
			if err := runBackup(context.Background(), os.Args[2:]); err != nil {
				slog.Error("backup", "err", err)
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
	// Parsed before the database is touched: a typo here is a misconfiguration
	// worth failing on, not something to log and run past.
	trustedProxies, err := config.ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		slog.Error("NETIS_TRUSTED_PROXIES", "err", err)
		os.Exit(1)
	}
	secretKey, err := config.ParseSecretKey(cfg.SecretKey)
	if err != nil {
		slog.Error("NETIS_SECRET_KEY", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(cfg.DSN, store.Options{
		MaxOpenConns: cfg.MaxOpenConns,
		MaxIdleConns: cfg.MaxIdleConns,
		SecretKey:    secretKey,
	})
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	broker := events.NewBroker()
	evs := events.NewService(st, broker)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Credentials entered before a key was configured are still plaintext on
	// disk; bring them under the key that now exists.
	if n, err := st.EncryptExistingSecrets(ctx); err != nil {
		slog.Error("encrypting stored secrets", "err", err)
		os.Exit(1)
	} else if n > 0 {
		slog.Info("encrypted stored secrets", "count", n)
	}

	// The offline threshold is not read here: the engine reads it from settings
	// on every sweep, so an admin's change takes effect without a restart.
	engine := &scan.Engine{
		Store: st, Events: evs, Broker: broker,
		Sweeper: scan.NewICMPSweeper(64),
		ARP:     scan.ReadARPTable,
		Resolve: scan.ResolveName,
	}
	sched := scan.NewScheduler(engine, st)
	go sched.Start(ctx)

	// Background loops that write to the store; shutdown waits on bg before
	// the deferred st.Close().
	var bg sync.WaitGroup
	runNow := newIntegrationRunner(st, evs)
	startIntegrationSyncs(ctx, &bg, runNow, time.Minute)
	startRetention(ctx, &bg, st, retentionInterval)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: web.NewServer(st, broker, sched, runNow, web.Options{
			TrustedProxies: trustedProxies,
			MetricsToken:   cfg.MetricsToken,
		}).Handler(),
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
	// Scans, integration syncs and retention run in their own goroutines, and
	// the deferred st.Close() is next: wait for the work the cancelled context
	// is unwinding so none of it writes into a closed database.
	sched.Wait()
	bg.Wait()
}
