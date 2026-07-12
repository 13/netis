# Netis — Hot-load Integration Settings (Sub-project F4): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Follow-on to F1. F1 made integration **run-now** read current settings on each
call; the **periodic** pollers still bind their client once at boot, so the
settings page shows "restart netis to apply integration changes". This removes
that limitation: periodic sync reads current settings each tick, so configuring
or changing an integration takes effect within a minute — no restart.

## Purpose

An integration configured (or edited) after startup is not polled automatically
until the process restarts, because `cmd/netis/main.go` builds each integration's
`Sync` once at boot inside `if url != "" { … go Sync.Start(ctx, time.Minute) }`.
Make the periodic poller reflect current settings, hot, and drop the restart
note.

## Approach: reuse the run-now closures on a ticker

F1's `newIntegrationRunner` already returns closures (`integrationRunner`, a
`map[string]func(context.Context) error`) that read current settings each call,
build the client, run `RunOnce` (which records the connected/failing
`IntegrationStatus`), and return `errNotConfigured` when the integration's URL is
empty. The periodic Start loop and RunOnce record status identically. So the
periodic poller becomes: for each integration, tick every minute and call the
same closure.

Replace the three boot `if url != "" { … Start … }` blocks with:

```go
var integrationNames = []string{"proxmox", "pihole", "wireguard"}

// startIntegrationSyncs periodically runs each integration from the current
// settings, so changes take effect without a restart. An unconfigured
// integration (errNotConfigured) is skipped silently each tick.
func startIntegrationSyncs(ctx context.Context, runNow integrationRunner, interval time.Duration) {
	for _, name := range integrationNames {
		go runIntegrationLoop(ctx, runNow, name, interval)
	}
}

func runIntegrationLoop(ctx context.Context, runNow integrationRunner, name string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := runNow.Run(ctx, name); err != nil && !errors.Is(err, errNotConfigured) {
			log.Printf("integration %s: %v", name, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
```

`main` calls `startIntegrationSyncs(ctx, runNow, time.Minute)` after building
`runNow`. Behavior preserved: each loop runs once immediately at boot (a
configured integration syncs and records status at boot, exactly as the old
`Start`'s pre-tick run did); an unconfigured one returns `errNotConfigured`,
records nothing, and is retried next tick — so once its settings are saved it
starts syncing within a minute.

The old blocks' direct `proxmox.NewSync(...).Start`, `pihole.NewSync(...).Start`,
and `wireguard.NewSSHRunner`/`NewSync(...).Start` calls are removed; those
packages remain imported (still used by `newIntegrationRunner`'s closures). The
`Sync.Start` methods themselves are left in place (still used by their own unit
tests); only main's call sites change.

## Settings note

`internal/web/views/settings.templ` line ~97 currently renders:
`<p class="muted">restart netis to apply integration changes</p>`. Replace with:
`<p class="muted">Integration changes apply automatically within a minute — or click Run now to apply immediately.</p>`. Regenerate `settings_templ.go`.

## Error handling

- Unconfigured integration each tick → `errNotConfigured`, skipped, not logged.
- Configured-but-unreachable → the closure's `RunOnce` records a failing status;
  the non-sentinel error is logged once per tick (`integration <name>: <err>`).
- Context cancellation → each loop returns promptly at the `select`.
- WireGuard SSH setup failure inside the closure → surfaces as a non-sentinel
  error, logged; retried next tick (self-healing once the host/key is fixed).

## Testing

- **main** (`cmd/netis/main_test.go`):
  - `TestRunIntegrationLoopRunsThenStops`: build an `integrationRunner{"x": …}`
    whose closure increments a counter and returns `errNotConfigured`; run
    `runIntegrationLoop(ctx, runner, "x", time.Hour)` in a goroutine; cancel
    `ctx`; assert the loop returned and the counter is exactly 1 (the immediate
    pre-tick run; the 1-hour interval guarantees no second run before cancel).
    Proves: runs immediately, honors cancellation, and `errNotConfigured` does
    not break the loop. Uses a `sync/atomic` counter and a short poll for
    completion (no fixed sleep on the tick).
  - The existing `TestIntegrationRunnerReadsCurrentSettings` (F1) already proves
    the closures read current settings — unchanged.

## Project layout (files added / modified)

- Modify: `cmd/netis/main.go` — add `integrationNames`, `startIntegrationSyncs`,
  `runIntegrationLoop`; replace the three boot blocks with one
  `startIntegrationSyncs(ctx, runNow, time.Minute)` call.
- Modify: `cmd/netis/main_test.go` — add `TestRunIntegrationLoopRunsThenStops`.
- Modify: `internal/web/views/settings.templ` (+ regenerated
  `settings_templ.go`) — the note.

## Out of scope (F4)

- Sub-minute apply / event-driven reload on save (the poll interval is the apply
  latency; run-now already applies instantly).
- Per-integration configurable intervals.
- Changing `Sync.Start` (kept for its own package tests) or the store.

## Global constraints

- No new dependencies; no store/schema change.
- The web package must not import proxmox/pihole/wireguard (wiring stays in main).
- Regenerate templ after editing `.templ`
  (`export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`); commit the
  regenerated `settings_templ.go` with source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Delete stray `netis`/`netis.db*` before finishing.
