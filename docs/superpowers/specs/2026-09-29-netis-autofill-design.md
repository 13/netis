# netis device autofill (A-series): design

Date: 2026-09-29. Status: A1 merged (plan
`docs/superpowers/plans/2026-09-29-netis-autofill-a1.md`); A2-A4 planned in
`docs/superpowers/plans/2026-09-30-netis-autofill-a2-a4.md`.

## Problem

A new device arrives with an IP, a MAC and maybe a hostname. Everything that
makes the inventory useful (vendor, model, kind, icon, function, tags) is typed
by hand. Today only `vendor` is filled automatically, from a 24-entry OUI
table, and only on devices a sweep creates (`scan.createUnknown`); devices from
DHCP leases get nothing. The network already says far more than that: the full
IEEE OUI registry, hostnames like `Galaxy-S23` or `BRW3C2AF4A1B2C3`, open ports
like 9100 or 8006, mDNS TXT records (`md=Chromecast`, `model=MacBookPro18,3`,
`ty=HP LaserJet`), and UPnP descriptions (`manufacturer`, `modelName`).

## Goals

- Fill vendor, model, kind, icon, function, tags and (for placeholder names)
  name from what netis can observe, without a user lifting a finger.
- Never overwrite what a person typed, imported or accepted. A value a user
  changes or removes stays theirs forever.
- Show where each filled value came from.

Non-goals: DHCP option 55/60 fingerprinting (the lease sources netis reads do
not expose options), SNMP, NetBIOS, HTTP banner grabbing, OS detection, cloud
lookups. No new event type.

## Series

- **A1 framework + local sources.** Hint store, resolver, apply rules with
  per-field ownership, the full IEEE OUI registry, hostname rules, open-port
  rules, a settings toggle, "detected" markers and a "What netis detected"
  panel on the device page. Runs after every sweep, every lease sync and a
  full pass at startup.
- **A2 mDNS services.** After a subnet sweep, browse DNS-SD on the scan
  interface and turn service types and TXT records into hints.
- **A3 SSDP/UPnP.** M-SEARCH on the scan interface, fetch each responder's
  description XML (only from the responder's own IP), hints from it.
- **A4 suggestions in the device form.** Datalists for vendor, model, function
  and tags built from existing values plus this device's hints; a "Use
  detected" link beside a field that has a hint the device does not show.

Each unit ships alone. A2 and A3 only add a source; they need no framework
change.

## Model

Two tables, migration `0016_autofill.sql` in both dialect directories.

```sql
-- One observation: source says field is probably value.
CREATE TABLE device_hint (
  device_id  INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  source     TEXT NOT NULL,   -- oui, hostname, ports, mdns, ssdp
  field      TEXT NOT NULL,   -- vendor, model, kind, icon, function, name, tag
  value      TEXT NOT NULL,
  confidence INTEGER NOT NULL, -- 0..100
  detail     TEXT NOT NULL DEFAULT '', -- evidence shown to people, e.g. "hostname BRW3C2A…"
  seen_at    TEXT NOT NULL,
  PRIMARY KEY (device_id, source, field, value)
);

-- What autofill wrote, so a later user change is recognisable.
CREATE TABLE device_autofill (
  device_id  INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  field      TEXT NOT NULL,   -- vendor, model, kind, icon, function, name, or tag:<name>
  value      TEXT NOT NULL,   -- the value autofill wrote
  source     TEXT NOT NULL,
  state      TEXT NOT NULL CHECK (state IN ('applied','owned')),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (device_id, field)
);
```

A source reports the complete set of hints it has for a device each time it
runs: `ReplaceHints(device, source, hints)` deletes that source's rows for the
device and inserts the new ones, in one transaction. Hints are never shared
between devices.

## Resolver

Pure function in a new package `internal/autofill`:
`Resolve(hints) -> map[field]Candidate` for single-valued fields and a tag set.

- Single-valued field: highest confidence wins; ties break by source rank
  `mdns > ssdp > ports > hostname > oui`, then by value for determinism.
- Only candidates with confidence >= 50 are applied. Lower hints are still
  stored and shown (and become form suggestions in A4).
- Tags: every tag hint with confidence >= 50.

## Apply rules

For each single-valued field with candidate `v`, current value `cur` and
autofill record `rec`:

1. `rec.state = owned`: skip. The person owns this field.
2. `rec` applied and `cur != rec.value`: a person changed it. Set
   `rec.state = owned`; skip.
3. `cur` is empty: write `v`, record `applied`.
4. `rec` applied, `cur == rec.value`, device not reviewed, `v != cur`: write
   `v` (a better source arrived), record `applied`.
5. Otherwise skip. That covers values typed by a person, set by an import or
   an integration, and any value on a reviewed device.

"Empty" is `""` for vendor, model, function and icon; `other` for kind; for
name, a placeholder while the device is unreviewed: `unknown-<mac>`,
`unknown-<ip>`, `private-<mac>` (scan) or `<source>-<mac>` (lease sources). Kind is only filled from `other`, so a kind
an integration set (vm, lxc, wg-peer, server) is never touched.

Tags are additive. For each tag candidate `t` with record `rec = tag:t`:
`owned` skips; `applied` but tag missing from the device means the person
removed it, so mark `owned`; no record adds the tag (auto-created as today by
`SetDeviceTags`, colour `#888888`) and records `applied`. Autofill never
removes a tag.

Writes go through one store method, `ApplyAutofill(ctx, deviceID, changes)`,
that updates the device columns without touching `reviewed` (unlike
`UpdateDevice`, which marks a device reviewed). Each run that changes a device
writes one audit entry: user `netis`, action `device.autofill`, target the
device, detail `vendor=Brother, kind=printer (hostname, oui)`.

## When it runs

`autofill.Service` has two entry points:

- `Run(ctx, deviceIDs...)` recomputes the local-source hints (oui, hostname,
  ports) for those devices from the database, then resolves and applies.
  With no ids it covers every device. A home inventory is a few hundred
  devices and each costs a handful of indexed queries, so a full pass is
  cheap.
- `Kick()` asks for a full pass without waiting. Kicks coalesce: a loop
  started by `Start(ctx)` runs one pass per burst of kicks, at most once
  every 10 s.

Callers: `Start` runs a full pass at startup (this is the backfill for
existing devices); `scan.Engine.RunSubnet` kicks after a sweep; the
integration runner kicks after a successful run; the port scan handler calls
`Run` for its device so the page it redirects to shows the result; saving
the setting kicks.

Setting `autofill_enabled`, default `on`, under Settings, Admin, Network
("Fill in device details automatically"). Off makes Run a no-op; hints and
records stay.

The scan engine's own `Vendor()` call in `createUnknown` goes: vendor now
comes through autofill like every other field.

## A1 sources

**OUI.** Move `internal/scan/oui.go` to `internal/oui`. Data is the IEEE
MA-L, MA-M and MA-S registries, fetched by `go run ./internal/oui/gen`
(`make oui`), normalised and committed as `internal/oui/oui.txt.gz`
(`prefix<TAB>name`, prefix 6, 7 or 9 hex digits). Lookup is longest prefix.
Normalising strips legal suffixes (`Inc.`, `Co., Ltd.`, `Corporation`,
`GmbH`, `Technologies`, …), fixes all-caps names to title case, and applies a
small override map for names people know (`Raspberry Pi Trading Ltd` →
`Raspberry Pi`, `Hon Hai Precision` → `Foxconn`, …). Private (locally
administered) MACs get no hint. Hints: vendor at 90. A short vendor-to-kind
list gives kind at 40 (below the threshold, so a suggestion only) for vendors
that make one thing: Espressif, Tuya, Shelly → iot; Brother, Epson → printer.

**Hostname.** Table-driven rules in `internal/autofill/hostname.go`, each a
case-insensitive regexp over every interface hostname and the device name,
with the hints it yields and a test case. Starting set:

| Pattern | Hints |
|---|---|
| `iphone` | kind phone 70, vendor Apple 70, model iPhone 60 |
| `ipad` | kind phone 60, icon tablet 70, vendor Apple 70, model iPad 60 |
| `macbook`, `imac`, `mac-?mini` | kind computer 70, vendor Apple 70, model from match 60 |
| `galaxy`, `^sm-[a-z]\d` | kind phone 70, vendor Samsung 70 |
| `^pixel` | kind phone 70, vendor Google 70 |
| `^android-` | kind phone 60 |
| `^(desktop\|laptop)-[a-z0-9]{7}$` | kind computer 70 (Windows default names) |
| `^brw\|^brn` + 12 hex | kind printer 80, vendor Brother 80 |
| `^hp[0-9a-f]{6}`, `^npi` | kind printer 80, vendor HP 80 |
| `epson` | kind printer 70, vendor Epson 70 |
| `^esp[-_]`, `tasmota`, `shelly`, `esphome` | kind iot 70, tag smart-home 60 |
| `chromecast`, `roku`, `appletv`, `firetv` | icon tv 70, tag media 60 |
| `sonos` | icon speaker 70, vendor Sonos 70, tag media 60 |
| `ps[45]`, `xbox`, `nintendo` | icon gamepad-2 70 |
| `raspberrypi` | kind computer 60, vendor Raspberry Pi 60 |
| `synology`, `diskstation`, `truenas`, `nas` | kind server 60, tag nas 60 |
| `^pve`, `proxmox` | kind server 70, function Proxmox VE 60 |

Evidence detail names the hostname that matched.

**Ports.** From stored `open_port` rows (on-demand scans), in
`internal/autofill/ports.go`:

| Open port | Hints |
|---|---|
| 9100 or 631 | kind printer 70 |
| 554 | icon cctv 60, tag camera 60 |
| 8006 | kind server 80, function Proxmox VE 80 |
| 8123 | function Home Assistant 70, tag smart-home 60 |
| 32400 | function Plex 70, tag media 60 |
| 3389 | kind computer 60 |
| 53 | function DNS 50 |
| 1883 | tag mqtt 50 |

## UI (A1)

- Device page: a field autofill filled and still holds shows a small
  "detected" badge; its tooltip names the source and evidence ("From the MAC
  vendor list", "From hostname BRW3C2A…").
- Device page: a "What netis detected" disclosure listing every hint as
  field, value, source, evidence, and whether it was applied, kept below the
  threshold, or held back because the person owns the field.
- Settings, Admin, Network: the toggle above.
- `docs/discovery.md` gains a section on autofill and ownership.

## Error handling

Rules are compiled with `regexp.MustCompile` at package init, and a test
compiles and exercises each one, so a bad pattern fails the build's tests,
not a sweep. A database error for one device is logged; Run moves on to the
next device and returns the first error, which its caller logs. Autofill
never fails a sweep or a lease sync.

## Testing

- `internal/oui`: longest-prefix lookup across MA-L/MA-M/MA-S, private MAC,
  normalisation table.
- `internal/autofill`: resolver ties and threshold; every hostname and port
  rule has a table test; apply rules 1-5 and the tag rules, each as a store
  test via `storetest.EachDialect`.
- Integration: a sweep creating `BRW3C2AF4A1B2C3` ends with kind printer and
  vendor Brother; a user edit to vendor followed by another sweep keeps the
  edit; removing an auto tag keeps it removed.
- Migration: `TestDeviceRebuildKeepsAddedColumns` still passes; new tables
  exist in both dialects.
- e2e: fixture device with hints; screenshot of the detected panel.

## A2-A4 notes (planned later)

- **A2 mDNS.** Query `_services._dns-sd._udp.local` on the scan interface,
  then PTR/SRV/TXT/A for the found service types, over a 3 s window, once per
  subnet sweep. Map by answering IP. Service types to hints: `_googlecast`
  (md= model, fn= name, icon tv, tag media), `_airplay`/`_raop` (model=,
  vendor Apple when model looks Apple), `_device-info` (model=), `_ipp`/
  `_printer`/`_pdl-datastream` (kind printer, ty= model, usb_MFG vendor),
  `_hap` (kind iot, ci= category to icon), `_hue` (vendor Philips, kind iot),
  `_esphomelib` (kind iot, function ESPHome), `_home-assistant` (function
  Home Assistant), `_smb` (tag file-share), `_sonos`, `_spotify-connect` (tag
  media). Instance names become name hints for placeholder names. Needs host
  networking in Docker; documented.
- **A3 SSDP.** `M-SEARCH ssdp:all`, 3 s window. Fetch LOCATION only when its
  host is the responder's IP, 2 s timeout, 64 KiB cap. `manufacturer` →
  vendor, `modelName`/`modelNumber` → model, `friendlyName` → name,
  `deviceType` → kind/icon (InternetGatewayDevice router, MediaRenderer tv,
  Printer printer, DigitalSecurityCamera icon cctv).
- **A4.** `store.DistinctDeviceValues(field)`; datalists in
  `device_form.templ`; "Use detected" fills the input client-side, and saving
  makes the value the person's.

## A2-A4 decisions (2026-09-30)

- **Package.** Network probes live in `internal/probe` and return raw
  observations (`MDNSService`, `UPnPDevice`); turning them into hints is
  `internal/autofill` (`probe_hints.go`), next to the other rule tables.
- **Mapping to devices.** An mDNS or SSDP answer belongs to the address it
  came from (the UDP packet source). The probe runs per subnet and maps
  addresses through `ListSubnetIfaceIPs`; an address netis has no device
  for is ignored (the next sweep creates it, the next probe fills it).
- **When.** The scan engine calls `Autofill.Probe(subnet)` after each sweep.
  The service queues it (non-blocking, dropped when the queue is full) and a
  worker probes each subnet at most once every 15 minutes, only when a local
  interface has an address in the subnet (a routed subnet gets nothing), and
  only while `autofill_enabled` is on. mDNS and SSDP run concurrently, 3 s
  window each. After storing hints the worker runs a pass for the devices
  that answered.
- **Stale probe hints.** A device that does not answer keeps its previous
  probe hints (it may be asleep). A device that answers replaces them.
- **mDNS queries.** One query with PTR questions (QU bit) for the fixed list
  of interesting service types, sent to 224.0.0.251:5353 out of the subnet's
  interface from an ephemeral port (legacy unicast, answered straight back,
  like the name lookup). An instance seen without TXT gets a follow-up TXT
  question. No service-type enumeration.
- **SSDP safety.** Description XML is fetched only when the LOCATION host is
  exactly the responder's IP, over http or https, no redirects, 2 s
  timeout, 64 KiB cap, at most 4 locations per responder.
- **Apple model identifiers** (`MacBookPro18,3`) become `MacBook Pro
  (MacBookPro18,3)` by prefix; unknown identifiers stay as they are.
- **A4 suggestions.** `store.DistinctDeviceValues(field)` for vendor,
  model and function (at most 200 each) plus this device's hint values feed
  `<datalist>`s. On the edit form, a field with a resolved candidate that
  differs from the current value (owned or not: the person decides) gets a
  "Use detected:
  <value>" button that fills the input client-side; tags get "Add
  detected tag" buttons that append to the comma list. Saving works as
  any edit: the value becomes the person's.
