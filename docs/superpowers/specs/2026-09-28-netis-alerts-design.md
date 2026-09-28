# Netis — Alerts and Notifications (F1): Design

Date: 2026-09-28
Status: Approved design (standing authorization), pre-implementation

## Purpose

netis records what happens on the network in its event log, but nobody reads a
log until something is already wrong. F1 pushes the handful of events that
matter on a home LAN to a phone or another system: a new device appeared, a
device you care about went offline or came back, two interfaces claim the same
IP, or an integration started or stopped failing.

## Decisions (locked)

- **Triggers** (each an event type; the notifier is fed from the events
  service, so anything that emits one is covered):
  - `device_new` — scan, Pi-hole, Proxmox and WireGuard discoveries.
  - `offline` / `online` — **only** for devices marked "alert when offline".
    The flag is per device, default off, toggled from the device page.
  - `ip_conflict` — new event type. After each subnet sweep the scan engine
    lists the IPs claimed by more than one interface and emits one event per IP
    that was not conflicting at the previous sweep of that subnet. The set is
    held in memory, so a conflict still present after a restart is announced
    once more. Manual assignments are picked up on the next sweep of the subnet.
  - `scan_error` (already raised once per outage) and `sync_recovered` — new
    event type raised by the integration runner when a failing integration
    succeeds again.
- **Channels**, both optional, both may be active at once:
  - Generic webhook: `POST` JSON, optional `Authorization` header value.
  - ntfy: `POST` plain text to the topic URL with `Title`, `Tags`, `Priority`
    and (when a base URL is set) `Click` headers; optional bearer token.
  - Gotify skipped: ntfy covers the self-hosted push case and Gotify can take a
    webhook through a bridge.
- **Secrets** (`notify_webhook_auth`, `notify_ntfy_token`) join
  `store.SecretSettings`, so they are encrypted at rest under
  `NETIS_SECRET_KEY` like the integration credentials, never echoed back into
  the form, and a blank field keeps the stored value (a "clear" checkbox
  removes it).
- **Per-type toggles**, grouped the way a user thinks about them: new devices,
  offline/online, IP conflicts, integration/scan errors. All default on — the
  offline group is already gated by the per-device flag, and nothing is sent
  until a channel is configured.
- **Base URL** (`notify_base_url`, optional) builds links to the device page or
  the event log. Without it messages carry no link.

## Component 1: store

- Migration `0010_alerts` (both dialects):
  - `device.alert_offline` boolean, default false.
  - The `event.type` CHECK constraint gains `ip_conflict` and `sync_recovered`
    (SQLite rebuilds the event table; Postgres swaps the named constraint).
- `SetDeviceAlertOffline(ctx, id, bool)` and `DeviceAlert(ctx, id) (name,
  alert, err)`: separate from `UpdateDevice`, so integration syncs and the edit
  form never touch the flag.
- `ConflictingIPs(ctx, subnetID) map[ip][]deviceName`.
- `migrate-db` treats `alert_offline` as a boolean column.

## Component 2: `internal/notify`

- `Notifier` implements the events service's new `Subscriber` hook
  (`Notify(type, deviceID, details)`), called after the event row is written.
- **Bounded async queue** (256). `Notify` never blocks: when the queue is full
  the event is dropped and logged. Filtering needs database reads, so it
  happens on the worker, not in `Emit`.
- Worker: reads the current settings per event (changes apply without a
  restart), drops types that are disabled and online/offline events for
  devices without the flag, resolves the device name, and adds the event to a
  pending batch. A subnet sweep raises `scan_error` on every failed attempt, so
  an identical `scan_error` is sent at most once an hour.
- **Coalescing**: the first event opens a 30 s window; when it closes the batch
  is handed to the sender. One event → a normal message; several → one summary
  ("12 new devices, 1 went offline") listing up to 20 lines. A scan that finds
  50 devices sends one message.
- Sender goroutine (hand-off channel of 4, dropped and logged when full, so a
  slow endpoint never stalls collection): each channel gets a 5 s timeout per
  attempt and up to 3 attempts with 1 s / 2 s backoff; a 4xx other than 429 is
  not retried.
- `SendTest(ctx, store)` sends a fixed test message synchronously to every
  configured channel and returns the per-channel errors.

### Payloads

Webhook, one event:

```json
{"event":"offline","device":{"id":3,"name":"nas"},"details":"nas (192.168.1.5) went offline",
 "time":"2026-09-28T10:00:00Z","url":"https://netis.lan/devices/3"}
```

Several events: `event` is `"batch"`, `details` the summary line, `url` the
event log, and `events` carries the single-event objects.

ntfy: body is the details (or the summary plus one line per event), `Title`
e.g. "netis: device offline", `Tags` an emoji short code per type, `Priority`
4 for offline, conflicts and errors, 3 for new/online, 2 for recoveries (a
batch takes its highest).

## Component 3: web

- Settings → **Notifications** tab, admin only (viewers get neither the tab
  nor the data; `?tab=notifications` falls back to Subnets for them). Its view
  lives in its own templ file.
- `POST /settings/notifications` (save) and `POST /settings/notifications/test`
  (htmx toast), both `requireAdmin`.
- `POST /devices/{id}/alert` toggles the flag from the device page,
  `requireAdmin`; the page shows the state to everyone and the button to admins.
- Events page filter gains the two new types.

## Testing

Notifier batching, per-type and per-device filtering, webhook/ntfy payloads
against `httptest`, retry on 5xx, a full queue not blocking `Emit`, secrets
encrypted at rest, conflict transition detection in the engine, recovered event
from the runner, admin-only save/test/toggle handlers (and the route walk).
