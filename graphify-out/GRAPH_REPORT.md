# Graph Report - .  (2026-07-21)

## Corpus Check
- 144 files · ~152,928 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1067 nodes · 2432 edges · 62 communities (51 shown, 11 thin omitted)
- Extraction: 82% EXTRACTED · 18% INFERRED · 0% AMBIGUOUS · INFERRED: 433 edges (avg confidence: 0.76)
- Token cost: 315,554 input · 0 output

## Community Hubs (Navigation)
- HTMX Vendor Library
- Web Auth & Login Tests
- Dashboard & Integration Status Docs
- Scan Engine & Scheduler
- Device Store Tests
- Port Scanning
- Grid & Subnet Handlers
- Settings Store & Templates
- Dashboard Handlers
- Auth Middleware & Rate Limiter
- Pi-hole API Client
- Subnet Auto-detection
- Pi-hole Sync
- WireGuard SSH Dump Parsing
- Device Store Model
- Device Templ Views
- Proxmox Client & Sync
- Event Broker Tests
- Event & Meta Store
- Initial SQL Schema
- Scan Handler Tests
- Main Entrypoint & Integration Loops
- Device Dialog UI Design
- Run-now Handler Tests
- Settings Handlers
- Onboarding & Settings Design Docs
- Event Broker Core
- Tags & Links Store
- Design System & Device List Plans
- Proxmox Client Tests
- Store Migrations Core
- Core Architecture Design Docs
- Integration Runner Evolution Docs
- Web Server & Routing
- Onboarding & Settings Plans
- Pi-hole & Lease Authority Docs
- Data Model & Scanning Design Docs
- Device Detail & List Design Docs
- App Config
- ARP Table Parsing
- Device Icons
- Theme & Migration Rationale Docs
- Availability Tracking Store
- SSE Handler
- Theme Toggle JS
- Name Resolution
- Toasts JS
- Device View Tests
- Pi-hole Source Migration
- Integration Status Migration
- Device Reviewed Migration
- Device Model/Function Migration
- Netis Binary

## God Nodes (most connected - your core abstractions)
1. `testServer()` - 87 edges
2. `authedGet()` - 59 edges
3. `authedPost()` - 34 edges
4. `openTest()` - 28 edges
5. `He()` - 28 edges
6. `t()` - 26 edges
7. `ie()` - 26 edges
8. `ce()` - 25 edges
9. `te()` - 24 edges
10. `e()` - 24 edges

## Surprising Connections (you probably didn't know these)
- `newIntegrationRunner()` --calls--> `NewSSHRunner()`  [INFERRED]
  cmd/netis/main.go → internal/wireguard/ssh.go
- `main()` --calls--> `Load()`  [INFERRED]
  cmd/netis/main.go → internal/config/config.go
- `main()` --calls--> `NewBroker()`  [INFERRED]
  cmd/netis/main.go → internal/events/broker.go
- `main()` --calls--> `NewService()`  [INFERRED]
  cmd/netis/main.go → internal/events/service.go
- `main()` --calls--> `NewScheduler()`  [INFERRED]
  cmd/netis/main.go → internal/scan/scheduler.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Integration Polling and Status Recording Pattern** — docs_superpowers_plans_2026_07_11_netis_implementation_proxmox_sync, docs_superpowers_plans_2026_07_11_netis_implementation_wireguard_sync, docs_superpowers_plans_2026_07_11_netis_pihole_implementation_pihole_sync, docs_superpowers_plans_2026_07_11_netis_dashboard_implementation_integration_status_table, docs_superpowers_plans_2026_07_12_netis_quick_fixes_newintegrationrunner [EXTRACTED 1.00]
- **Lease Kind (static/dhcp) Management Flow** — docs_superpowers_plans_2026_07_11_netis_pihole_implementation_upsertipassignment, docs_superpowers_plans_2026_07_11_netis_pihole_implementation_reservation_beats_lease, docs_superpowers_plans_2026_07_12_netis_subnet_grid_v2_setipkind, docs_superpowers_plans_2026_07_12_netis_device_detail_overhaul_lease_toggle, docs_superpowers_plans_2026_07_12_netis_lease_persist_fix_static_downgrade_guard [INFERRED 0.85]
- **Reusable Templ Component Extraction** — docs_superpowers_plans_2026_07_12_netis_device_list_tree_sortable_devicetable, docs_superpowers_plans_2026_07_12_netis_subnet_devices_devicerow, docs_superpowers_plans_2026_07_12_netis_subnets_index_subnetcard [INFERRED 0.75]
- **UI/UX Modernization Series (A design system, B device list, C dialog)** — docs_superpowers_specs_2026_07_12_netis_design_system_design, docs_superpowers_specs_2026_07_12_netis_device_list_design, docs_superpowers_specs_2026_07_12_netis_device_dialog_design [EXTRACTED 1.00]
- **Integration Runner Evolution (E1 registry, F1 current-settings closures, F4 periodic hot-reload)** — docs_superpowers_specs_2026_07_12_netis_integration_run_now_design_integration_runner, docs_superpowers_specs_2026_07_12_netis_quick_fixes_design_new_integration_runner, docs_superpowers_specs_2026_07_12_netis_hotload_integrations_design_periodic_hot_reload [EXTRACTED 1.00]
- **ip_assignment.kind Authority Flow (Pi-hole upsert, grid toggle, detail toggle, non-downgrade rule)** — docs_superpowers_specs_2026_07_11_netis_pihole_design_upsert_ip_assignment, docs_superpowers_specs_2026_07_12_netis_subnet_grid_v2_design_lease_kind_toggle, docs_superpowers_specs_2026_07_12_netis_device_detail_overhaul_design_lease_toggle, docs_superpowers_specs_2026_07_12_netis_lease_persist_fix_design_non_downgrade_upsert [EXTRACTED 1.00]

## Communities (62 total, 11 thin omitted)

### Community 0 - "HTMX Vendor Library"
Cohesion: 0.08
Nodes (108): a(), Ae(), an(), at(), B(), be(), bn(), bt() (+100 more)

### Community 1 - "Web Auth & Login Tests"
Cohesion: 0.06
Nodes (101): addAdmin(), Server, Store, T, TestLoginBrand(), TestLoginRateLimit(), TestLoginSetsSessionCookie(), TestRedirectToSetupWhenNoUsers() (+93 more)

### Community 2 - "Dashboard & Integration Status Docs"
Cohesion: 0.05
Nodes (56): SSE-refreshed Dashboard Fragment, integration_status Table, Needs-Attention Panel, Dashboard Enhancements Plan, recordStatus/runAndCount Pattern, Single Go Monolith Architecture, Netis Implementation Plan, Proxmox Client and Sync (+48 more)

### Community 3 - "Scan Engine & Scheduler"
Cohesion: 0.07
Nodes (38): Context, Store, Time, Context, Store, T, TestAutoCreatesUnknownDevice(), TestAvailabilityStableAcrossIPChange() (+30 more)

### Community 4 - "Device Store Tests"
Cohesion: 0.09
Nodes (40): T, strp(), TestDeviceIfaceIP(), TestDeviceModelFunctionAndKinds(), TestDeviceReviewedFromSource(), TestListDevicesCarriesLeaseKindAndReviewed(), TestMigration0004Backfill(), TestRemoveIfaceIPsKeepsStatic() (+32 more)

### Community 5 - "Port Scanning"
Cohesion: 0.10
Nodes (20): Addr, Context, Duration, PortScan(), ServiceGuess(), T, TestPortScanFindsListener(), TestServiceGuess() (+12 more)

### Community 6 - "Grid & Subnet Handlers"
Cohesion: 0.11
Nodes (17): Store, Store, Request, ResponseWriter, Server, Request, ResponseWriter, Server (+9 more)

### Community 7 - "Settings Store & Templates"
Cohesion: 0.11
Nodes (20): Store, Store, defaultScanEnabled(), generalOfflineAfter(), generalTab(), Component, integrationsFields(), integrationsTab() (+12 more)

### Community 8 - "Dashboard Handlers"
Cohesion: 0.11
Nodes (25): userFrom(), Request, ResponseWriter, Server, Request, ResponseWriter, Server, Dashboard() (+17 more)

### Community 9 - "Auth Middleware & Rate Limiter"
Cohesion: 0.12
Nodes (21): HandlerFunc, clientIP(), Handler, Mutex, Request, ResponseWriter, Time, Server (+13 more)

### Community 10 - "Pi-hole API Client"
Cohesion: 0.15
Nodes (19): Context, Mutex, NewClient(), normIP(), normMAC(), fixtureServer(), Server, T (+11 more)

### Community 11 - "Subnet Auto-detection"
Cohesion: 0.13
Nodes (18): detectFrom(), DetectSubnets(), Prefix, hasVirtualPrefix(), systemInterfaces(), Prefix, T, mustPrefix() (+10 more)

### Community 12 - "Pi-hole Sync"
Cohesion: 0.16
Nodes (22): Context, Duration, Store, NewSync(), subnetForIP(), deviceByName(), Store, T (+14 more)

### Community 13 - "WireGuard SSH Dump Parsing"
Cohesion: 0.11
Nodes (17): ClientConfig, Time, ParseDump(), T, TestParseDump(), Context, NewSSHRunner(), Context (+9 more)

### Community 14 - "Device Store Model"
Cohesion: 0.12
Nodes (6): Store, scanDevice(), Device, Iface, IPRow, SubnetIfaceIP

### Community 15 - "Device Templ Views"
Cohesion: 0.23
Nodes (22): DeviceDialog(), DeviceDrawer(), deviceFormInner(), DeviceList(), DevicePage(), deviceRow(), deviceTable(), Component (+14 more)

### Community 16 - "Proxmox Client & Sync"
Cohesion: 0.19
Nodes (10): Client, Context, Context, Duration, Store, NewSync(), Client, Guest (+2 more)

### Community 17 - "Event Broker Tests"
Cohesion: 0.20
Nodes (12): NewBroker(), T, TestPubSub(), TestUnsubscribeStopsDelivery(), NewSync(), Context, T, TestRunOnceRespectsContextCancellation() (+4 more)

### Community 18 - "Event & Meta Store"
Cohesion: 0.14
Nodes (8): Store, anyOnline(), CustomField, Event, Link, OpenPort, DeviceDetail, IfaceDetail

### Community 19 - "Initial SQL Schema"
Cohesion: 0.23
Nodes (15): availability_history, custom_field, device, device_link, device_tag, event, iface, iface_status (+7 more)

### Community 20 - "Scan Handler Tests"
Cohesion: 0.29
Nodes (12): Mutex, Server, Store, T, TestScanAllTriggersNonWireGuard(), TestScanButtonsRendered(), TestScanNowLanTriggersAndToasts(), TestScanNowNonexistent404() (+4 more)

### Community 21 - "Main Entrypoint & Integration Loops"
Cohesion: 0.27
Nodes (11): Context, Duration, Store, main(), newIntegrationRunner(), runIntegrationLoop(), startIntegrationSyncs(), T (+3 more)

### Community 22 - "Device Dialog UI Design"
Cohesion: 0.19
Nodes (14): Reusable Component Set (.card/.pill/.chip/.seg/.dialog), Edit Drawer (right-side panel), Device Create/Edit Dialog Design (C), Device Create/Edit Dialog, dialog.js Modal Controller, SetDeviceTags Tag Sync, Device List Overhaul Design (B), List/Grid View Toggle (localStorage) (+6 more)

### Community 23 - "Run-now Handler Tests"
Cohesion: 0.24
Nodes (11): Context, Mutex, Server, Store, T, TestIntegrationRunBadName(), TestIntegrationRunNilRunner(), TestIntegrationRunPihole() (+3 more)

### Community 24 - "Settings Handlers"
Cohesion: 0.35
Nodes (4): Request, ResponseWriter, Server, parseSubnetForm()

### Community 25 - "Onboarding & Settings Design Docs"
Cohesion: 0.24
Nodes (12): CSS Design Token System, General Settings Expansion Design (G3), New-Subnet Defaults (general settings), Onboarding & Settings Clarity Design, netdetect Subnet Auto-Detection, Tabbed Settings Page, First-Run Welcome Wizard (onboarded gate), Onboarding Redesign Design (E3) (+4 more)

### Community 26 - "Event Broker Core"
Cohesion: 0.26
Nodes (6): Broker, Msg, Service, Mutex, Store, NewService()

### Community 28 - "Design System & Device List Plans"
Cohesion: 0.20
Nodes (11): Token-based Light/Dark Design System, Design System Foundation Plan (A), Persisted Theme Toggle, Sortable Dual-view Device Table, Device List Overhaul Plan (B), Reviewed Flag and Approve Action, Shared deviceTable Templ, GroupByParent Helper (+3 more)

### Community 29 - "Proxmox Client Tests"
Cohesion: 0.35
Nodes (9): NewClient(), fixtureServer(), Server, T, TestGuestMACs(), TestListGuests(), T, TestProxmoxRunOnceStats() (+1 more)

### Community 30 - "Store Migrations Core"
Cohesion: 0.31
Nodes (5): DB, T, TestEmitWritesAndBroadcasts(), Store, Open()

### Community 31 - "Core Architecture Design Docs"
Cohesion: 0.22
Nodes (10): Dashboard Widgets Fragment, Netis Core Design (Home Network Organizer), Single Go Monolith Architecture, Proxmox Integration, SSE Live Updates (HTMX), Subnet Grid View, WireGuard Integration (SSH wg show dump), Full-Range Grid with Edge Squares (AllIPs) (+2 more)

### Community 32 - "Integration Runner Evolution Docs"
Cohesion: 0.24
Nodes (10): Hot-load Integration Settings Design (F4), Periodic Integration Hot-Reload, Integration Run-now + Settings Alignment Design (E1), IntegrationRunner Registry, Integration Run-now Button + Toast Feedback, Lease-Persist Fix Design (G1), Quick Fixes Design (F1: nav order + run-now), newIntegrationRunner (current-settings closures) (+2 more)

### Community 33 - "Web Server & Routing"
Cohesion: 0.39
Nodes (7): Handler, Store, Server, NewServer(), ServeMux, IntegrationRunner, ScanTrigger

### Community 34 - "Onboarding & Settings Plans"
Cohesion: 0.29
Nodes (8): New-subnet Defaults in General Settings, General Settings Expansion Plan (G3), netdetect Subnet Auto-detection, Onboarding and Settings Clarity Plan, Settings Tabs, First-run Quick-setup Wizard, onboardShell Branded Stepper, Onboarding Redesign Plan (E3)

### Community 35 - "Pi-hole & Lease Authority Docs"
Cohesion: 0.39
Nodes (8): Dashboard Enhancements Design, integration_status Table & Store, Pi-hole Integration Design, Pi-hole v6 API Client, Pi-hole Reconciliation Sync, UpsertIPAssignment Store Method, Non-Downgrading UpsertIPAssignment (static wins), Static/DHCP Lease Kind Toggle (SetIPKind)

### Community 36 - "Data Model & Scanning Design Docs"
Cohesion: 0.32
Nodes (8): Netis SQLite Data Model, Scan Engine (ICMP/ARP sweep pipeline), GroupByParent Tree Grouping, Richer Device Model Design (C1), Model & Function Device Fields (migration 0005), Scanning UX Design (C2), shouldRunScan Manual-Bypass Guard, OUI Vendor Database (MAC prefix to vendor)

### Community 37 - "Device Detail & List Design Docs"
Cohesion: 0.25
Nodes (8): Device-Detail Overhaul Design (F2), Device Detail Hero + Two-Column Layout, Per-IP Lease Toggle (detail page), Server-side Device Sorting, Device-List Parent/Child + Sortable Subnet List Design (F3), Shared Sortable deviceTable (base-path links), Subnet Page Devices List + CRUD Design (E2), Devices-in-Subnet Scoped List

### Community 38 - "App Config"
Cohesion: 0.43
Nodes (5): Config, Load(), T, TestLoadDefaults(), TestLoadFromEnv()

### Community 39 - "ARP Table Parsing"
Cohesion: 0.33
Nodes (5): ParseARPTable(), ReadARPTable(), T, TestParseARPTable(), Reader

### Community 40 - "Device Icons"
Cohesion: 0.43
Nodes (5): DeviceIcon(), KindIcon(), T, TestDeviceIcon(), TestKindIcon()

### Community 41 - "Theme & Migration Rationale Docs"
Cohesion: 0.33
Nodes (6): Transactional Migrate Loop (FK-off rebuild), Design System Foundation & Theme Toggle Design (A), Emoji Device Icons, Theme Toggle (localStorage, no-flash bootstrap), Icon Picker (DeviceIcon + IconChoices), Router & Modem Device Kinds

### Community 43 - "SSE Handler"
Cohesion: 0.50
Nodes (3): Request, ResponseWriter, Server

## Knowledge Gaps
- **35 isolated node(s):** `netis`, `setting`, `device_new`, `integration_status`, `device` (+30 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **11 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `testServer()` connect `Web Auth & Login Tests` to `Event Broker Tests`, `Run-now Handler Tests`, `Store Migrations Core`, `Web Server & Routing`?**
  _High betweenness centrality (0.139) - this node is a cross-community bridge._
- **Why does `Subnet` connect `Grid & Subnet Handlers` to `Scan Engine & Scheduler`, `Settings Store & Templates`, `Dashboard Handlers`, `Pi-hole Sync`, `WireGuard SSH Dump Parsing`, `Device Templ Views`, `Settings Handlers`?**
  _High betweenness centrality (0.114) - this node is a cross-community bridge._
- **Why does `NewServer()` connect `Web Server & Routing` to `Web Auth & Login Tests`, `Auth Middleware & Rate Limiter`, `Pi-hole API Client`, `Scan Handler Tests`, `Main Entrypoint & Integration Loops`, `Run-now Handler Tests`, `Event Broker Core`, `Proxmox Client Tests`?**
  _High betweenness centrality (0.095) - this node is a cross-community bridge._
- **Are the 76 inferred relationships involving `testServer()` (e.g. with `NewBroker()` and `Open()`) actually correct?**
  _`testServer()` has 76 INFERRED edges - model-reasoned connections that need verification._
- **Are the 50 inferred relationships involving `authedGet()` (e.g. with `addAdmin()` and `authedPost()`) actually correct?**
  _`authedGet()` has 50 INFERRED edges - model-reasoned connections that need verification._
- **Are the 17 inferred relationships involving `authedPost()` (e.g. with `authedGet()` and `TestCellDetailAndSetKind()`) actually correct?**
  _`authedPost()` has 17 INFERRED edges - model-reasoned connections that need verification._
- **Are the 23 inferred relationships involving `openTest()` (e.g. with `TestDeviceIfaceIP()` and `TestDeviceModelFunctionAndKinds()`) actually correct?**
  _`openTest()` has 23 INFERRED edges - model-reasoned connections that need verification._