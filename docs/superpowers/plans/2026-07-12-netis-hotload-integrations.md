# Netis Hot-load Integration Settings (Sub-project F4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The periodic integration pollers read current settings each tick, so configuring/editing an integration applies within a minute — no restart; drop the settings "restart to apply" note.

**Architecture:** Reuse F1's `newIntegrationRunner` closures (which already read current settings and record status) on a per-integration ticker in `cmd/netis/main.go`, replacing the boot-time `Sync.Start` blocks. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `$(go env GOPATH)/bin/templ`), `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change.
- The web package must NOT import proxmox/pihole/wireguard (integration wiring stays in `cmd/netis/main.go`).
- Regenerate templ after editing `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `settings_templ.go` with source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` before finishing.

---

### Task 1: Periodic pollers read current settings each tick

**Files:**
- Modify: `cmd/netis/main.go`
- Modify: `cmd/netis/main_test.go`
- Modify: `internal/web/views/settings.templ` (+ regenerated `settings_templ.go`)

**Interfaces:**
- Produces: `func startIntegrationSyncs(ctx context.Context, runNow integrationRunner, interval time.Duration)`; `func runIntegrationLoop(ctx context.Context, runNow integrationRunner, name string, interval time.Duration)`; `var integrationNames []string`.
- Consumes: F1's `integrationRunner` (`Run(ctx, name)`), `errNotConfigured`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/netis/main_test.go` (add `"sync/atomic"` and `"time"` to imports if absent):

```go
func TestRunIntegrationLoopRunsThenStops(t *testing.T) {
	var calls int32
	runner := integrationRunner{
		"x": func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return errNotConfigured
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runIntegrationLoop(ctx, runner, "x", time.Hour) // long interval: only the immediate run fires
		close(done)
	}()
	// The loop runs the closure once immediately, before waiting on the ticker.
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("loop never ran the closure")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return after context cancel")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("closure called %d times, want exactly 1 (immediate run only)", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/netis/ -run TestRunIntegrationLoopRunsThenStops -v`
Expected: FAIL to compile — `runIntegrationLoop` undefined.

- [ ] **Step 3: Add the loop helpers**

In `cmd/netis/main.go`, add at top level (near `newIntegrationRunner`). Ensure `"errors"` is imported (F1 added it):

```go
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

- [ ] **Step 4: Replace the boot blocks with one call**

In `cmd/netis/main.go`'s `main`, delete the three `if …URL/…Addr != "" { … go X.Start(ctx, time.Minute) }` blocks (proxmox, wireguard, pihole) and replace them with a single line, keeping the existing `runNow := newIntegrationRunner(st, evs)` above it:

```go
	runNow := newIntegrationRunner(st, evs)
	startIntegrationSyncs(ctx, runNow, time.Minute)
```

Leave the `srv := web.NewServer(st, broker, sched, runNow)` line unchanged. After deletion, confirm `log`, `time`, `context`, and the three integration packages are still imported (they are: `log`/`time` used by the loop, the packages by `newIntegrationRunner`).

- [ ] **Step 5: Update the settings note**

In `internal/web/views/settings.templ`, change:

```
	<p class="muted">restart netis to apply integration changes</p>
```

to:

```
	<p class="muted">Integration changes apply automatically within a minute — or click Run now to apply immediately.</p>
```

- [ ] **Step 6: Regenerate templ, run tests + full build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean; `TestRunIntegrationLoopRunsThenStops` and the F1 `TestIntegrationRunnerReadsCurrentSettings` pass; all packages green.

- [ ] **Step 7: Commit**

```bash
gofmt -w cmd/netis/main.go cmd/netis/main_test.go && go vet ./...
git add cmd/netis/main.go cmd/netis/main_test.go internal/web/views/settings.templ internal/web/views/settings_templ.go
git commit -m "$(printf 'feat: hot-load integration settings; periodic sync reads current config\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `integrationNames` + `startIntegrationSyncs` + `runIntegrationLoop` reusing the run-now closures on a ticker → Task 1. ✅
- Boot `Start` blocks replaced by one `startIntegrationSyncs` call → Task 1. ✅
- Settings note updated → Task 1. ✅
- Test: immediate run + cancel-exit + errNotConfigured-safe → Task 1. ✅
- Out of scope (Sync.Start removal, per-integration intervals, event-driven reload) → untouched. ✅

**Placeholder scan:** none — every step shows complete code.

**Type consistency:** `runIntegrationLoop(ctx, runNow integrationRunner, name string, interval time.Duration)` matches its `startIntegrationSyncs` call and the test call. `integrationRunner.Run(ctx, name)` and `errNotConfigured` are the F1 definitions (`main` package). `startIntegrationSyncs(ctx, runNow, time.Minute)` matches the `main` call site. The test constructs `integrationRunner{"x": func(context.Context) error}` — the literal map type.

**Ordering note:** single task; the immediate-run-then-cancel test avoids ticker-timing flakiness by using a 1-hour interval so only the pre-tick run fires before cancellation.
