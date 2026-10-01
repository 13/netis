# Graph Report - netis  (2026-10-01)

## Corpus Check
- 354 files · ~565,695 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 3407 nodes · 18584 edges · 245 communities (121 shown, 124 thin omitted)
- Extraction: 91% EXTRACTED · 9% INFERRED · 0% AMBIGUOUS · INFERRED: 1675 edges (avg confidence: 0.84)
- Token cost: 184,898 input · 0 output

## Community Hubs (Navigation)
- Main Entrypoint & Backups
- Web Handler Tests
- Store Tests & Backup Runs
- Startup Wiring & Sync Tests
- Scan Engine & Device Store
- HTMX Vendor Library
- Healthcheck, OIDC & Config Tests
- Subnet Grid & Free Ranges
- DHCP Lease Clients
- Device & API Handlers
- API Token Auth Tests
- Autofill Probe Tests
- Settings Templates
- Rate Limiting & Proxy Trust
- Settings & Tag Handlers
- Auth Middleware & Error Pages
- Device Detail Views
- Notifications
- Device Filters & Private MACs
- Grid & Device List JS
- Sweep Targets & Grid Handler
- Pi-hole Client
- Event Emitter & Backup Scheduler
- Scan Engine Events
- Shared Templ Components
- Scan Engine Tests
- Tag & Audit Handler Tests
- E2E Playwright Setup
- OIDC Login Flow
- Users, Tokens & Secrets Store
- Build Info & Metrics
- Probe Hint Rules
- WireGuard SSH
- Dashboard Views
- Autofill Resolver
- Routing & DHCP Pool Parsing
- Probe Hint Tests
- Autofill Service
- mDNS Browsing
- Login & Onboarding Views
- SSDP/UPnP Probe
- SQL Dialect Layer
- Event Views
- CI, Release & Dependabot
- Tag Settings Views
- Device Form
- DHCP Sources & WoL Design
- Tag Editor JS
- Device Hint Sources
- Onboarding Subnet Detection
- Notifier Tests
- Upstream Reconciliation
- Security Docs
- Name Resolution
- Discovery & Integrations Docs
- Secret Encryption
- Dashboard Handlers
- Write API
- API & Export Docs
- Autofill Plans
- Device List Filtering
- Device List Views
- Proxmox Client & Sync
- Backups Docs
- Dashboard & Status Plans
- API Tokens Design
- Autofill Design
- OPNsense Integration
- Scan Scheduler
- CSV Import
- Device Detail & Grid Plans
- UI Redesign & Tags Design
- Wake-on-LAN
- Subnet Auto-detection
- Audit Log
- Scan & Onboarding Tests
- Command Palette JS
- Alerts Design
- Scheduler Tests
- Settings & Onboarding Design
- Pi-hole Plans
- Design System & Device List Plans
- Core Architecture Design
- Component Set Design
- Event Broker
- Pi-hole Sync Tests
- Availability Bars
- Database Migration Tool
- Free Static IPs Plan
- Pi-hole & WireGuard Design
- OIDC Design
- OUI Registry
- CSV/JSON Export
- Integration Status Design
- Device Detail Design
- TypeScript Config
- Port Scanning
- Dialogs & Copy JS
- Onboarding & General Settings Plans
- Integration Runner Plans
- Roles & Audit Design
- Docs Link Tests
- Favicon Generator
- Admin Route Tests
- 2026 07 12 Netis Grid Click Device Design (small)
- 2026 09 28 Netis Scheduled Backups Design (small)
- 2026 10 01 Netis Free Ips Design (small)
- Device Detected (small)
- E2E Fixture Test (small)
- Grid (small)
- Importexport (small)
- Contrast Test (small)
- Theme (small)
- Auditlabel (small)
- Dump (small)
- 2026 09 30 Netis Tags (small)
- 2026 09 28 Netis Discovery Design (small)
- 2026 07 12 Netis Device Model Fields Design (small)
- Arp (small)
- Sse (small)
- Time (small)
- Toasts (small)
- Sources (small)
- Server Test (small)
- Devices Test (small)
- Sweep (small)
- Autofill Test (small)
- Main Test (small)
- Api (small)
- Settings (small)
- Go (small)

## God Nodes (most connected - your core abstractions)
1. `testServer()` - 293 edges
2. `authedGet()` - 173 edges
3. `Store` - 118 edges
4. `authedPost()` - 95 edges
5. `Open()` - 59 edges
6. `eachDialect()` - 56 edges
7. `Subnet` - 56 edges
8. `Icon()` - 53 edges
9. `addAdmin()` - 42 edges
10. `NewService()` - 40 edges

## Surprising Connections (you probably didn't know these)
- `templ generate drift check` --semantically_similar_to--> `Make targets (build, test, test-pg, e2e, screenshots)`  [INFERRED] [semantically similar]
  .github/workflows/ci.yml → docs/development.md
- `CI job (vet, staticcheck, race tests, govulncheck, build)` --references--> `PostgreSQL backend`  [INFERRED]
  .github/workflows/ci.yml → docs/backups.md
- `Device Reviewed Flag + Approve` --shares_data_with--> `Netis SQLite Data Model`  [INFERRED]
  docs/superpowers/specs/2026-07-12-netis-device-list-design.md → docs/superpowers/specs/2026-07-11-netis-design.md
- `GroupByParent Tree Grouping` --references--> `Netis SQLite Data Model`  [INFERRED]
  docs/superpowers/specs/2026-07-12-netis-device-list-tree-sortable-design.md → docs/superpowers/specs/2026-07-11-netis-design.md
- `shouldRunScan Manual-Bypass Guard` --semantically_similar_to--> `newIntegrationRunner (current-settings closures)`  [INFERRED] [semantically similar]
  docs/superpowers/specs/2026-07-12-netis-scanning-ux-design.md → docs/superpowers/specs/2026-07-12-netis-quick-fixes-design.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Integration Polling and Status Recording Pattern** — docs_superpowers_plans_2026_07_11_netis_implementation_proxmox_sync, docs_superpowers_plans_2026_07_11_netis_implementation_wireguard_sync, docs_superpowers_plans_2026_07_11_netis_pihole_implementation_pihole_sync, docs_superpowers_plans_2026_07_11_netis_dashboard_implementation_integration_status_table, docs_superpowers_plans_2026_07_12_netis_quick_fixes_newintegrationrunner [EXTRACTED 1.00]
- **Lease Kind (static/dhcp) Management Flow** — docs_superpowers_plans_2026_07_11_netis_pihole_implementation_upsertipassignment, docs_superpowers_plans_2026_07_11_netis_pihole_implementation_reservation_beats_lease, docs_superpowers_plans_2026_07_12_netis_subnet_grid_v2_setipkind, docs_superpowers_plans_2026_07_12_netis_device_detail_overhaul_lease_toggle, docs_superpowers_plans_2026_07_12_netis_lease_persist_fix_static_downgrade_guard [INFERRED 0.85]
- **Reusable Templ Component Extraction** — docs_superpowers_plans_2026_07_12_netis_device_list_tree_sortable_devicetable, docs_superpowers_plans_2026_07_12_netis_subnet_devices_devicerow, docs_superpowers_plans_2026_07_12_netis_subnets_index_subnetcard [INFERRED 0.75]
- **UI/UX Modernization Series (A design system, B device list, C dialog)** — docs_superpowers_specs_2026_07_12_netis_design_system_design, docs_superpowers_specs_2026_07_12_netis_device_list_design, docs_superpowers_specs_2026_07_12_netis_device_dialog_design [EXTRACTED 1.00]
- **Integration Runner Evolution (E1 registry, F1 current-settings closures, F4 periodic hot-reload)** — docs_superpowers_specs_2026_07_12_netis_integration_run_now_design_integration_runner, docs_superpowers_specs_2026_07_12_netis_quick_fixes_design_new_integration_runner, docs_superpowers_specs_2026_07_12_netis_hotload_integrations_design_periodic_hot_reload [EXTRACTED 1.00]
- **ip_assignment.kind Authority Flow (Pi-hole upsert, grid toggle, detail toggle, non-downgrade rule)** — docs_superpowers_specs_2026_07_11_netis_pihole_design_upsert_ip_assignment, docs_superpowers_specs_2026_07_12_netis_subnet_grid_v2_design_lease_kind_toggle, docs_superpowers_specs_2026_07_12_netis_device_detail_overhaul_design_lease_toggle, docs_superpowers_specs_2026_07_12_netis_lease_persist_fix_design_non_downgrade_upsert [EXTRACTED 1.00]
- **Autofill hint pipeline (sources -> hints -> resolve -> apply)** — docs_superpowers_plans_2026_09_29_netis_autofill_a1_local_sources, docs_superpowers_plans_2026_09_30_netis_autofill_a2_a4_probe_hints, docs_superpowers_plans_2026_09_29_netis_autofill_a1_device_hint, docs_superpowers_plans_2026_09_29_netis_autofill_a1_resolve, docs_superpowers_plans_2026_09_29_netis_autofill_a1_device_autofill_record, docs_superpowers_plans_2026_09_29_netis_autofill_a1_autofill_service [EXTRACTED 1.00]
- **DHCP lease integrations merged by MAC** — docs_integrations_pihole, docs_integrations_adguard_home, docs_integrations_opnsense, docs_integrations_sync_runner, docs_integrations_never_overwrite_edits [EXTRACTED 1.00]
- **Free static IP feature** — docs_superpowers_plans_2026_10_01_netis_free_ips_dhcp_pool, docs_superpowers_plans_2026_10_01_netis_free_ips_free_for_static_rule, docs_superpowers_plans_2026_10_01_netis_free_ips_free_ranges, docs_superpowers_plans_2026_10_01_netis_free_ips_search_free, docs_superpowers_plans_2026_10_01_netis_free_ips_port_hover_card, docs_usage_subnet_grid, docs_usage_dashboard, docs_usage_command_palette [INFERRED 0.95]
- **Migration chain 0010-0018 extending device/event schema** — docs_superpowers_specs_2026_09_28_netis_alerts_design_migration_0010_alerts, docs_superpowers_specs_2026_09_28_netis_api_tokens_design_migration_0011_api_token, docs_superpowers_specs_2026_09_28_netis_wol_stale_design_migration_0012_upstream_missing, docs_superpowers_specs_2026_09_28_netis_roles_audit_design_audit_log_table, docs_superpowers_specs_2026_09_28_netis_oidc_design_migration_0014, docs_superpowers_specs_2026_09_28_netis_dhcp_sources_design_migration_0015_dhcp_sources, docs_superpowers_specs_2026_09_30_netis_tags_design_migration_0017_tag_colors, docs_superpowers_specs_2026_10_01_netis_free_ips_design_migration_0018_dhcp_pool [INFERRED 0.85]
- **Never overwrite user-set values (ownership rules)** — docs_superpowers_specs_2026_09_28_netis_api_tokens_design_enrich_dont_clobber, docs_superpowers_specs_2026_09_29_netis_autofill_design_apply_rules, docs_superpowers_specs_2026_09_28_netis_wol_stale_design_syncintegrationips, docs_superpowers_specs_2026_09_28_netis_dhcp_sources_design_leases_apply, docs_superpowers_specs_2026_09_28_netis_discovery_design_setifacehostnameifempty [INFERRED 0.85]
- **Autofill hint sources** — docs_superpowers_specs_2026_09_29_netis_autofill_design_oui, docs_superpowers_specs_2026_09_29_netis_autofill_design_hostname_rules, docs_superpowers_specs_2026_09_29_netis_autofill_design_port_rules, docs_superpowers_specs_2026_09_29_netis_autofill_design_a2_mdns, docs_superpowers_specs_2026_09_29_netis_autofill_design_a3_ssdp [EXTRACTED 1.00]

## Communities (245 total, 124 thin omitted)

### Community 0 - "Main Entrypoint & Backups"
Cohesion: 0.23
Nodes (9): Group, SessionMeta, adminKey, pathKey, apiDeviceCreate, apiEvent, apiSubnet, auditKey (+1 more)

### Community 1 - "Web Handler Tests"
Cohesion: 0.02
Nodes (208): TestAboutTabReportsBuildAndRuntime(), TestAboutTabRequiresAuth(), TestFooterLinksToAbout(), TestGeneralTabSavesRetention(), TestOldSettingsLinksRedirect(), TestDeviceDeleteRemovesDevice(), TestEventsPageFiltersByType(), TestFieldSetAndDelete() (+200 more)

### Community 2 - "Store Tests & Backup Runs"
Cohesion: 0.03
Nodes (143): runBackup(), TestBackupDoesNotMigrateTheSource(), TestBackupMissingSourceCreatesNothing(), TestBackupPostgresHintRedactsPassword(), TestBackupProducesAReadableDatabase(), TestBackupRedirectsPostgresToPgDump(), TestBackupRefusesADirectorySource(), TestBackupRefusesToOverwrite() (+135 more)

### Community 3 - "Startup Wiring & Sync Tests"
Cohesion: 0.04
Nodes (112): startBackups(), backupConfig(), TestStartBackupsDisabledWithoutDir(), TestStartBackupsSkipsPostgres(), TestStartBackupsWritesAndStops(), failureCategory(), main(), newIntegrationRunner() (+104 more)

### Community 4 - "Scan Engine & Device Store"
Cohesion: 0.03
Nodes (30): Engine, TCPProbe(), tcpProbe(), TestTCPProbe(), Store, Store, Iface, IPRow (+22 more)

### Community 5 - "HTMX Vendor Library"
Cohesion: 0.08
Nodes (108): a(), Ae(), an(), at(), B(), be(), bn(), bt() (+100 more)

### Community 6 - "Healthcheck, OIDC & Config Tests"
Cohesion: 0.06
Nodes (51): healthAddr(), runHealthcheck(), TestHealthAddr(), TestRunHealthcheck(), TestRunHealthcheckNoServer(), envInt(), Load(), ParseSecretKey() (+43 more)

### Community 7 - "Subnet Grid & Free Ranges"
Cohesion: 0.07
Nodes (53): Subnet, TestFreeRanges(), freeRanges(), pluralWord(), CellInfo, FreeRange, GridPageData, GridStats (+45 more)

### Community 8 - "DHCP Lease Clients"
Cohesion: 0.07
Nodes (29): Client, DHCP, Fetcher, lease, Stats, Sync, Subscriber, entries() (+21 more)

### Community 9 - "Device & API Handlers"
Cohesion: 0.08
Nodes (14): Server, TestLocalNext(), TestSubnetForPicksMostSpecific(), Server, isHTMX(), parseTags(), portScanToast(), redirectAfterForm() (+6 more)

### Community 10 - "API Token Auth Tests"
Cohesion: 0.08
Nodes (41): bearer(), getAs(), TestBearerExemptFromOriginChecks(), TestBearerFailuresRateLimited(), TestBearerTokenAuthenticatesAPI(), TestCreateTokenFromSettings(), TestDeletedUsersTokensStopWorking(), TestTokenRevokeScopedToOwner() (+33 more)

### Community 11 - "Autofill Probe Tests"
Cohesion: 0.09
Nodes (42): fakeProber(), printerAnswers(), sourceHints(), TestProbeLoopRateLimitsAndSkipsRouted(), TestProbeLoopSkipsIPv6(), TestProbeOffDoesNothing(), TestProbeSubnetSkipsUnchanged(), TestProbeSubnetStoresHintsAndFills() (+34 more)

### Community 12 - "Settings Templates"
Cohesion: 0.08
Nodes (45): APIToken, IntegrationStatus, Session, unscanned(), TestRoleLabel(), orDash(), shortAgent(), TestTimeComponent() (+37 more)

### Community 13 - "Rate Limiting & Proxy Trust"
Cohesion: 0.06
Nodes (38): newRateLimiter(), addAdmin(), newTrustingServer(), TestClientIPUsesForwardedHeaderFromTrustedProxy(), TestLoginBrand(), TestLoginPageLandmarks(), TestLoginRateLimit(), TestLoginRateLimitIsPerForwardedClient() (+30 more)

### Community 14 - "Settings & Tag Handlers"
Cohesion: 0.13
Nodes (12): newAPIToken(), TestTokenFormat(), auditNote(), clearSessionCookie(), isAdmin(), userFrom(), Server, validNewPassword() (+4 more)

### Community 15 - "Auth Middleware & Error Pages"
Cohesion: 0.07
Nodes (17): Server, auditAction(), patternTarget(), forMachines(), Server, limitKey(), onboardingAllowed(), peerAddr() (+9 more)

### Community 16 - "Device Detail Views"
Cohesion: 0.10
Nodes (40): Icon(), kindName(), aboutPanel(), anyOnline(), detectedBadge(), detectedPanel(), deviceActivityPanel(), deviceHeader() (+32 more)

### Community 17 - "Notifications"
Cohesion: 0.09
Nodes (21): LoadConfig(), buildMessage(), testMessage(), SendTest(), TestRetriesServerErrorsButNotClientErrors(), TestSummaryListsAtMostMaxLines(), newSender(), statusError() (+13 more)

### Community 18 - "Device Filters & Private MACs"
Cohesion: 0.07
Nodes (28): AnyPrivate(), IsPrivate(), TestIsPrivate(), DeviceRow, IPInfo, applyDeviceFilter(), deviceMatches(), ipConflicts() (+20 more)

### Community 19 - "Grid & Device List JS"
Cohesion: 0.11
Nodes (27): applyCols(), applySelection(), applyView(), boxes(), chosenCols(), get(), selected(), setup() (+19 more)

### Community 20 - "Sweep Targets & Grid Handler"
Cohesion: 0.10
Nodes (18): ValidURL(), TestValidURL(), TestAllIPsRefusesOversizedPrefix(), AllIPs(), HostIPs(), NewICMPSweeper(), TestAllIPs(), TestHostIPs() (+10 more)

### Community 21 - "Pi-hole Client"
Cohesion: 0.11
Nodes (19): NewClient(), normIP(), normMAC(), fixtureServer(), TestCloseLogsOutAndClosesConnections(), TestDNSRecords(), TestLeases(), TestNormMACRejectsGarbage() (+11 more)

### Community 22 - "Event Emitter & Backup Scheduler"
Cohesion: 0.09
Nodes (14): Emitter, Scheduler, Snapshotter, Status, syncFile(), Store, dayLabel(), dayStart() (+6 more)

### Community 23 - "Scan Engine Events"
Cohesion: 0.09
Nodes (15): recordingEmitter, Engine, logStoreErr(), macConflict(), namesWanted(), Engine, probeFailure(), SubnetIfaceIP (+7 more)

### Community 24 - "Shared Templ Components"
Cohesion: 0.11
Nodes (34): deviceIcon(), relatedPanel(), relatedRow(), deviceTile(), missingUpstreamPill(), upstreamName(), gridLegend(), iconTone() (+26 more)

### Community 25 - "Scan Engine Tests"
Cohesion: 0.08
Nodes (31): conflictEvents(), TestSweepAnnouncesNewIPConflictsOnce(), TestAutoCreatesUnknownDevice(), TestAvailabilityStableAcrossIPChange(), testEngine(), TestIPChangeDetectedByMAC(), TestKnownIPWithDifferentMACIsNewDevice(), TestKnownIPWithoutARPStaysMatched() (+23 more)

### Community 26 - "Tag & Audit Handler Tests"
Cohesion: 0.10
Nodes (27): viewerSession(), auditEntries(), postForm(), TestAuditFallsBackToPattern(), TestAuditRecordsChangesAndRefusals(), TestAuditRecordsLogins(), viewerSessionFor(), TestExportCSV() (+19 more)

### Community 27 - "E2E Playwright Setup"
Cohesion: 0.09
Nodes (21): description, devDependencies, @axe-core/playwright, @playwright/test, name, private, scripts, test (+13 more)

### Community 28 - "OIDC Login Flow"
Cohesion: 0.12
Nodes (12): newToken(), Server, oidcFlowOrErr(), ssoUsername(), TestSSOUsernameUsesOnlyVerifiedEmail(), LoginOptions, auditRecord, claimBool (+4 more)

### Community 29 - "Users, Tokens & Secrets Store"
Cohesion: 0.09
Nodes (6): Store, isSecretSetting(), Store, User, hashToken(), nullable()

### Community 30 - "Build Info & Metrics"
Cohesion: 0.10
Nodes (18): Dep, Get(), Info, pickDeps(), StartTime(), TestGetFillsRuntimeFacts(), TestLabel(), TestPickDeps() (+10 more)

### Community 31 - "Probe Hint Rules"
Cohesion: 0.22
Nodes (27): appleFamily, portRule, appleModel(), cleanAnnounced(), finish(), firstNonEmpty(), hAirplay(), hCompanionLink() (+19 more)

### Community 32 - "WireGuard SSH"
Cohesion: 0.11
Nodes (13): hostKeyCallback(), NewSSHRunner(), knownHostsLine(), TestNewSSHRunnerKeyErrors(), TestNewSSHRunnerRejectsUnreadableKnownHosts(), TestNewSSHRunnerVerifiesAgainstKnownHosts(), TestNewSSHRunnerWithoutKnownHosts(), TestRunHonoursContextDuringHandshake() (+5 more)

### Community 33 - "Dashboard Views"
Cohesion: 0.12
Nodes (26): activityPanel(), attentionActions(), attentionLabel(), attentionPanel(), attentionRow(), attentionTone(), Dashboard(), DashboardBody() (+18 more)

### Community 34 - "Autofill Resolver"
Cohesion: 0.14
Nodes (21): Candidate, better(), current(), decide(), Explain(), isEmpty(), IsPlaceholderName(), recordsByField() (+13 more)

### Community 35 - "Routing & DHCP Pool Parsing"
Cohesion: 0.10
Nodes (16): TestParseDHCPPool(), BackupStatus, Server, parseDHCPPool(), poolAddr(), DirectedBroadcast(), HostsOf(), SendAll() (+8 more)

### Community 36 - "Probe Hint Tests"
Cohesion: 0.14
Nodes (24): named, want, mdnsHints(), TestAppleModel(), TestGooglecastIcon(), TestHAPBridgeOnlyTags(), TestHAPCategoryIcons(), TestMDNSHints() (+16 more)

### Community 37 - "Autofill Service"
Cohesion: 0.11
Nodes (10): SubnetProber, Service, auditDetail(), Enabled(), Service, hintsEqual(), AutofillChanges, AutofillTag (+2 more)

### Community 38 - "mDNS Browsing"
Cohesion: 0.18
Nodes (16): browseMDNS(), buildQuestions(), buildTXTQuery(), parseTXT(), newResponder(), newResponderHosts(), TestBrowseMDNSFollowsUpForTXT(), TestBrowseMDNSInlineTXT() (+8 more)

### Community 39 - "Login & Onboarding Views"
Cohesion: 0.12
Nodes (24): authShell(), LoginPage(), onboardBrand(), onboardShell(), onboardStepper(), SetupPage(), TestSentence(), errorBody() (+16 more)

### Community 40 - "SSDP/UPnP Probe"
Cohesion: 0.18
Nodes (17): mdnsProber(), ssdpProber(), BrowseMDNS(), fetchAllowed(), fetchDescription(), SearchSSDP(), searchSSDP(), ssdpClient() (+9 more)

### Community 41 - "SQL Dialect Layer"
Cohesion: 0.17
Nodes (5): Store, Store, likeEscape(), scanEvents(), conn

### Community 42 - "Event Views"
Cohesion: 0.13
Nodes (21): Event, EventFilter, dayHeading(), eventDays(), eventDevice(), eventItem(), EventStatus(), EventDetail() (+13 more)

### Community 43 - "CI, Release & Dependabot"
Cohesion: 0.13
Nodes (22): Dependabot config, GitHub Actions version updates, go-minor-and-patch Dependabot group, CI job (vet, staticcheck, race tests, govulncheck, build), Coverage summary (excluding templ), CI workflow, Docker build-only job, e2e job (Playwright visual + a11y) (+14 more)

### Community 44 - "Tag Settings Views"
Cohesion: 0.13
Nodes (21): TagCount, tagChipID(), tagColorLabel(), tagColorPicker(), tagDeleteConfirm(), tagDevicesLabel(), tagMergeQuestion(), tagRenameAction() (+13 more)

### Community 45 - "Device Form"
Cohesion: 0.15
Nodes (14): Tag, DeviceDialog(), deviceFormInner(), DeviceFormPage(), DeviceForm, iconPicker(), tagNamesJoin(), tagsJSON() (+6 more)

### Community 46 - "DHCP Sources & WoL Design"
Cohesion: 0.18
Nodes (20): More DHCP Lease Sources (AdGuard Home, OPNsense) Design, AdGuard Home Integration (internal/adguard), AdoptDiscoveredIface, OPNsense ARP Enrichment, ClaimDHCPLease, Device Rebuild Keeps Added Columns Test, leases.Apply (internal/leases), Migration 0015_dhcp_sources (+12 more)

### Community 47 - "Tag Editor JS"
Cohesion: 0.21
Nodes (18): autoColor(), clean(), icon(), init(), add(), chipEl(), close(), colorOf() (+10 more)

### Community 48 - "Device Hint Sources"
Cohesion: 0.15
Nodes (12): deviceHints(), deviceTypeShort(), isMediaSoftware(), ssdpHints(), TestSSDPHintsDetail(), fetch(), main(), Normalize() (+4 more)

### Community 49 - "Onboarding Subnet Detection"
Cohesion: 0.16
Nodes (6): TestCheckSubnetSize(), CheckSubnetSize(), configuredIntegrations(), detectedSubnet(), Server, hostCount()

### Community 50 - "Notifier Tests"
Cohesion: 0.20
Nodes (18): New(), device(), endpoint(), jsonNum(), next(), none(), openStore(), running() (+10 more)

### Community 51 - "Upstream Reconciliation"
Cohesion: 0.13
Nodes (11): Store, SubnetIP, changeIDs(), TestConformanceReconcileUpstream(), allowedIPs(), TestValidIface(), ValidIface(), UpstreamChange (+3 more)

### Community 52 - "Security Docs"
Cohesion: 0.16
Nodes (18): Personal API tokens, NETIS_TRUSTED_PROXIES, Randomized (private) MAC detection, Installing netis doc, Login rate limiter, Behind a reverse proxy, Audit log, Break-glass local admin login (+10 more)

### Community 53 - "Name Resolution"
Cohesion: 0.22
Nodes (15): buildMDNSQuery(), ifaceFor(), mdnsLookupAddr(), parseMDNSAnswer(), ResolveName(), reverseDNS(), reverseName(), mdnsReply() (+7 more)

### Community 54 - "Discovery & Integrations Docs"
Cohesion: 0.20
Nodes (17): Configuration doc, Settings key/value table, Discovery doc, Presence without ping (TCP/ARP fallback), Background scan loop (ping sweep + ARP read), Sandboxed systemd unit (Proxmox LXC), AdGuard Home integration, Integrations doc (+9 more)

### Community 55 - "Secret Encryption"
Cohesion: 0.16
Nodes (11): TestNotifySecretsEncryptedAtRest(), eachDialectWithKey(), openPGSchema(), BenchmarkListDevices(), newCrypter(), mustCrypter(), TestEncryptedSecretWithoutTheRightKeyIsAnError(), testKey() (+3 more)

### Community 56 - "Dashboard Handlers"
Cohesion: 0.14
Nodes (12): Now(), failureDetail(), Server, eventDayBound(), Server, AttentionConflict, IntegrationName(), TestIntegrationName() (+4 more)

### Community 57 - "Write API"
Cohesion: 0.21
Nodes (9): TagNamesFit(), ValidTagName(), toAPIDevice(), Server, deviceTarget(), writeFailure(), validTagName(), apiDevice (+1 more)

### Community 58 - "API & Export Docs"
Cohesion: 0.18
Nodes (16): JSON API endpoints (/api/devices, /api/search, ...), CSV import with preview, CSV/JSON export, JSON API, export and metrics doc, NETIS_METRICS_TOKEN, Prometheus /metrics endpoint, Notifications doc, Notification batching (30s, bounded queue) (+8 more)

### Community 59 - "Autofill Plans"
Cohesion: 0.23
Nodes (16): Filling in device details (autofill), Reverse DNS and mDNS naming, Docker with --network host, autofill_enabled setting, autofill.Service (Run, Kick, Start), device_autofill record (applied/owned), device_hint table / store.Hint, Device autofill A1 plan (+8 more)

### Community 60 - "Device List Filtering"
Cohesion: 0.15
Nodes (13): clearTagHref(), filterDevices(), hasEmptyParams(), lowestIP(), parseDeviceFilter(), parseDeviceSort(), sortDeviceRows(), freeQuery() (+5 more)

### Community 61 - "Device List Views"
Cohesion: 0.21
Nodes (15): deviceCount(), KindLabel(), subnetLabel(), activeTagFilter(), activeTagFilterBody(), activeTagFilterOOB(), bulkBar(), bulkReturn() (+7 more)

### Community 62 - "Proxmox Client & Sync"
Cohesion: 0.21
Nodes (4): Client, Guest, Stats, Sync

### Community 63 - "Backups Docs"
Cohesion: 0.22
Nodes (15): Backups and database backends doc, netis migrate-db importer, netis backup (VACUUM INTO snapshot), pg_dump for Postgres backups, PostgreSQL backend, Restoring a SQLite backup, Scheduled backups, SQLite backend (+7 more)

### Community 64 - "Dashboard & Status Plans"
Cohesion: 0.16
Nodes (13): integration_status Table, Needs-Attention Panel, Dashboard Enhancements Plan, Netis Implementation Plan, Proxmox Client and Sync, Scan Engine and Scheduler, Session Auth and First-run Setup, SQLite Store with Embedded Migrations (+5 more)

### Community 65 - "API Tokens Design"
Cohesion: 0.23
Nodes (14): API Tokens, Write API, Export and CSV Import (F2) Design, Personal API Tokens (netis_ prefix), Bearer Authentication on /api/*, checkNewDevice, CreateDeviceWithIface, CSV Import with Dry Run, Inventory Export (JSON/CSV), Failed Token Lookup Rate Limit (+6 more)

### Community 66 - "Autofill Design"
Cohesion: 0.28
Nodes (14): Randomized (Private) MAC Detection (macaddr.IsPrivate), Device Autofill (A-series) Design, A2 mDNS Services Source, A3 SSDP/UPnP Source, A4 Form Suggestions (DistinctDeviceValues), ApplyAutofill, device_autofill Table (applied/owned), device_hint Table (+6 more)

### Community 67 - "OPNsense Integration"
Cohesion: 0.20
Nodes (13): NewClient(), discovered(), newSync(), strp(), TestARP(), TestLeasesAllEmpty(), TestLeasesErrors(), TestLeasesFallsBackToISC() (+5 more)

### Community 68 - "Scan Scheduler"
Cohesion: 0.17
Nodes (7): TestShouldRunScan(), Status, shouldRunScan(), finished, Scheduler, ScanStatusReporter, statusTrigger

### Community 69 - "CSV Import"
Cohesion: 0.23
Nodes (10): ImportMayRename(), TestFillDevice(), normMAC(), Server, importDetail(), parseImport(), splitList(), uncell() (+2 more)

### Community 70 - "Device Detail & Grid Plans"
Cohesion: 0.15
Nodes (14): SSE-refreshed Dashboard Fragment, SSE Event Broker, DeviceDrawer Edit Drawer, LeaseToggle (per-IP static/dhcp), Device-Detail Overhaul Plan (F2), DeviceDialog Modal Fragment, Device Create/Edit Dialog Plan (C), Grid Square Click: New/Edit/Open Device (+6 more)

### Community 71 - "UI Redesign & Tags Design"
Cohesion: 0.23
Nodes (12): UI Redesign (U-series) Design Brief, Colour Design Tokens, IBM Plex Sans/Mono Self-hosted, Settings Account/Admin Information Architecture, Patch Panel Subnet Grid, Tags Redesign (T-series) Design, Migration 0017_tag_colors, RenameTag (merge) (+4 more)

### Community 72 - "Wake-on-LAN"
Cohesion: 0.22
Nodes (9): InterfaceFor(), TestInterfaceForLoopback(), BuildMagicPacket(), Send(), SendTo(), TestBroadcastAddrIsTheDiscardPort(), TestBuildMagicPacket(), TestSendToDeliversTheMagicPacket() (+1 more)

### Community 73 - "Subnet Auto-detection"
Cohesion: 0.24
Nodes (9): detectFrom(), DetectSubnets(), hasVirtualPrefix(), systemInterfaces(), mustPrefix(), TestDetectFromFiltersAndDedupes(), TestDetectSubnetsRunsAgainstHost(), Detected (+1 more)

### Community 74 - "Audit Log"
Cohesion: 0.20
Nodes (7): AuditEntry, Store, Server, auditPageURL(), AuditData, AuditTarget, AuditFilter

### Community 75 - "Scan & Onboarding Tests"
Cohesion: 0.18
Nodes (11): currentStep(), TestOnboardingFlowSteps(), TestScanAllTriggersNonWireGuard(), TestScanButtonsRendered(), TestScanNowLanTriggersAndToasts(), TestScanNowNonexistent404(), TestScanNowSaysWhenNotQueued(), TestScanNowWireGuardDoesNotTrigger() (+3 more)

### Community 76 - "Command Palette JS"
Cohesion: 0.30
Nodes (12): close(), fetchRemote(), findFree(), icon(), matches(), newDevice(), open(), render() (+4 more)

### Community 77 - "Alerts Design"
Cohesion: 0.31
Nodes (12): Alerts and Notifications (F1) Design, Per-device Alert-When-Offline Flag, ip_conflict Event Type, Migration 0010_alerts, Settings Notifications Tab (admin only), Notifier (internal/notify), ntfy Notification Channel, store.SecretSettings (encrypted at rest under NETIS_SECRET_KEY) (+4 more)

### Community 78 - "Scheduler Tests"
Cohesion: 0.28
Nodes (9): newBlockingSweeper(), TestParallelScansAreBounded(), testScheduler(), TestStatusFollowsAScan(), TestSubnetNotScannedTwiceAtOnce(), TestSubnetsScanInParallel(), TestTriggerReportsWhetherItQueued(), waitFor() (+1 more)

### Community 79 - "Settings & Onboarding Design"
Cohesion: 0.24
Nodes (12): CSS Design Token System, General Settings Expansion Design (G3), New-Subnet Defaults (general settings), Onboarding & Settings Clarity Design, netdetect Subnet Auto-Detection, Tabbed Settings Page, First-Run Welcome Wizard (onboarded gate), Onboarding Redesign Design (E3) (+4 more)

### Community 80 - "Pi-hole Plans"
Cohesion: 0.27
Nodes (8): Pi-hole v6 API Client, Pi-hole Sync Reconciliation, Pi-hole Integration Plan, UpsertIPAssignment, Device model/function Fields (Migration 0005), Richer Device Model Plan (C1), Lease-Persist Fix Plan (G1), SetIPKind

### Community 81 - "Design System & Device List Plans"
Cohesion: 0.20
Nodes (11): Token-based Light/Dark Design System, Design System Foundation Plan (A), Persisted Theme Toggle, Sortable Dual-view Device Table, Device List Overhaul Plan (B), Reviewed Flag and Approve Action, Shared deviceTable Templ, GroupByParent Helper (+3 more)

### Community 82 - "Core Architecture Design"
Cohesion: 0.22
Nodes (10): Dashboard Widgets Fragment, Netis Core Design (Home Network Organizer), Netis SQLite Data Model, Scan Engine (ICMP/ARP sweep pipeline), SSE Live Updates (HTMX), Subnet Grid View, Subnet Occupancy Grid v2 Design (C3), Full-Range Grid with Edge Squares (AllIPs) (+2 more)

### Community 83 - "Component Set Design"
Cohesion: 0.24
Nodes (9): Design System Foundation & Theme Toggle Design (A), Reusable Component Set (.card/.pill/.chip/.seg/.dialog), Device Create/Edit Dialog Design (C), Device Create/Edit Dialog, Icon Picker (DeviceIcon + IconChoices), SetDeviceTags Tag Sync, Device List Overhaul Design (B), List/Grid View Toggle (localStorage) (+1 more)

### Community 84 - "Event Broker"
Cohesion: 0.27
Nodes (5): Broker, Msg, NewBroker(), TestPubSub(), TestUnsubscribeStopsDelivery()

### Community 85 - "Pi-hole Sync Tests"
Cohesion: 0.25
Nodes (10): deviceByName(), strpP(), TestDNSRecordAttachesAndSkipsUnknown(), TestPiholeLeaseKeepsManualStatic(), TestPiholeRunOnceStats(), TestReservationBeatsLease(), testSync(), TestSyncCreatesUnknownFromReservation() (+2 more)

### Community 86 - "Availability Bars"
Cohesion: 0.33
Nodes (11): availBars(), availBarTitle(), availSummary(), BuildAvailability(), fmtPct(), availabilityPanel(), TestBuildAvailability(), TestFmtPct() (+3 more)

### Community 87 - "Database Migration Tool"
Cohesion: 0.29
Nodes (8): TestMigrateDBBadDestinationRedactsPassword(), copyTable(), firstNonEmpty(), relinkDeviceParents(), resetIdentity(), runMigrateDB(), TestToBool(), toBool()

### Community 88 - "Free Static IPs Plan"
Cohesion: 0.31
Nodes (10): Subnets (CIDR, scan interval), First-run /setup flow, Subnet DHCP pool (InDHCPPool), Free static IPs (F) plan, Free-for-static rule (freeSummary, grid free/pool states), freeRanges, Grid port hover card, searchFree / palette free query (+2 more)

### Community 89 - "Pi-hole & WireGuard Design"
Cohesion: 0.27
Nodes (9): WireGuard Integration (SSH wg show dump), Pi-hole Integration Design, Pi-hole v6 API Client, Pi-hole Reconciliation Sync, UpsertIPAssignment Store Method, Per-IP Lease Toggle (detail page), Model & Function Device Fields (migration 0005), Lease-Persist Fix Design (G1) (+1 more)

### Community 90 - "OIDC Design"
Cohesion: 0.36
Nodes (8): OpenID Connect Login (F7a) Design, Role Mapping via NETIS_OIDC_ADMIN_GROUP, Authorization Code Flow with PKCE, OIDC Identity Mapping (iss, sub), Migration 0014 oidc_issuer/oidc_subject, netis_oidc Signed Cookie, OIDC Environment Configuration, Link SSO Account (POST /settings/sso/link)

### Community 91 - "OUI Registry"
Cohesion: 0.24
Nodes (4): lookup(), TestLookupLongestPrefix(), TestVendorRealRegistry(), Vendor()

### Community 92 - "CSV/JSON Export"
Cohesion: 0.36
Nodes (6): csvCell(), exportFilename(), Server, exportDevice, exportIface, exportIP

### Community 93 - "Integration Status Design"
Cohesion: 0.31
Nodes (9): Dashboard Enhancements Design, integration_status Table & Store, Hot-load Integration Settings Design (F4), Periodic Integration Hot-Reload, Integration Run-now + Settings Alignment Design (E1), IntegrationRunner Registry, Integration Run-now Button + Toast Feedback, newIntegrationRunner (current-settings closures) (+1 more)

### Community 94 - "Device Detail Design"
Cohesion: 0.22
Nodes (9): Device-Detail Overhaul Design (F2), Device Detail Hero + Two-Column Layout, Server-side Device Sorting, Device-List Parent/Child + Sortable Subnet List Design (F3), Shared Sortable deviceTable (base-path links), GroupByParent Tree Grouping, Quick Fixes Design (F1: nav order + run-now), Subnet Page Devices List + CRUD Design (E2) (+1 more)

### Community 95 - "TypeScript Config"
Cohesion: 0.22
Nodes (8): compilerOptions, esModuleInterop, module, noEmit, skipLibCheck, strict, target, include

### Community 96 - "Port Scanning"
Cohesion: 0.28
Nodes (4): PortScan(), ServiceGuess(), TestPortScanFindsListener(), TestServiceGuess()

### Community 97 - "Dialogs & Copy JS"
Cohesion: 0.28
Nodes (4): cidrBits(), cleanup(), closeModal(), ip4()

### Community 98 - "Onboarding & General Settings Plans"
Cohesion: 0.29
Nodes (8): New-subnet Defaults in General Settings, General Settings Expansion Plan (G3), netdetect Subnet Auto-detection, Onboarding and Settings Clarity Plan, Settings Tabs, First-run Quick-setup Wizard, onboardShell Branded Stepper, Onboarding Redesign Plan (E3)

### Community 99 - "Integration Runner Plans"
Cohesion: 0.25
Nodes (8): Hot-loaded Integration Poller Settings, Hot-load Integration Settings Plan (F4), Integration Run-now Plan (E1), Run-now Runner Registry, newIntegrationRunner Closures, Quick Fixes Plan (F1), Scanning UX Plan (C2), Toast HTML Fragments

### Community 100 - "Roles & Audit Design"
Cohesion: 0.43
Nodes (8): Roles and Audit Log (F5) Design, audit_log Table (Migration 0013), Server.audit Route-pattern Middleware, Settings Audit Tab (keyset pagination), auditNote, Store.PruneAudit / audit_retention_days, Per-request Role Lookup (GetSession joins user), SetUserRoleGuarded

### Community 101 - "Docs Link Tests"
Cohesion: 0.46
Nodes (6): docLinks(), headingAnchors(), slug(), TestDocsLinks(), TestSlug(), docLink

### Community 102 - "Favicon Generator"
Cohesion: 0.50
Nodes (5): fail(), main(), png(), run(), write()

### Community 103 - "Admin Route Tests"
Cohesion: 0.36
Nodes (7): registeredRoutes(), TestAdminRoutesRefuseViewersAndAnonymous(), TestMutatingRoutesAreAdminOnly(), TestRegisteredRoutesMatchMux(), isMutating(), TestEveryMutatingRouteHasAnAuditAction(), route

### Community 104 - "2026 07 12 Netis Grid Click Device Design (small)"
Cohesion: 0.33
Nodes (7): Edit Drawer (right-side panel), dialog.js Modal Controller, Grid Square Click Device Design (G2), Occupied Square Popup Open/Edit Actions, Free Grid Square Opens Prefilled New Device, Toast Feedback System (#toasts, ScanToast), CellDetail Popup

### Community 105 - "2026 09 28 Netis Scheduled Backups Design (small)"
Cohesion: 0.52
Nodes (6): Scheduled Backups (F4) Design, NETIS_BACKUP_DIR/INTERVAL/KEEP, Backup Pruning (keep newest N), Backup Scheduler (internal/backup), Backup Status / Metrics, Store.VacuumInto (VACUUM INTO)

### Community 106 - "2026 10 01 Netis Free Ips Design (small)"
Cohesion: 0.43
Nodes (7): Command Palette, Free Static IPs (F) Design, Subnet DHCP Pool (dhcp_start/dhcp_end), freeSummary / Free for Static Use, store.Subnet.InDHCPPool, Migration 0018_dhcp_pool, Palette 'free' Query

### Community 107 - "Device Detected (small)"
Cohesion: 0.43
Nodes (4): Explained, fieldValue(), DeviceDetail, sourceLabel()

### Community 108 - "E2E Fixture Test (small)"
Cohesion: 0.33
Nodes (7): Freeze(), TestFreezeAndRestore(), seedE2E(), stripSQLComments(), TestE2ESeedRenders(), TestE2EServe(), TestRelTimeFollowsTheClock()

### Community 109 - "Grid (small)"
Cohesion: 0.33
Nodes (4): IPClaim, Occupant, Store, freeSummary()

### Community 110 - "Importexport (small)"
Cohesion: 0.29
Nodes (4): Store, DeviceFill, ExportIface, ExportIP

### Community 111 - "Contrast Test (small)"
Cohesion: 0.29
Nodes (6): contrast(), cssTokens(), luminance(), TestTokenContrast(), colorTokens(), Server

### Community 112 - "Theme (small)"
Cohesion: 0.62
Nodes (6): apply(), choose(), mark(), stored(), system(), tint()

### Community 113 - "Auditlabel (small)"
Cohesion: 0.33
Nodes (7): auditTab(), filterSelect(), AuditActionLabel(), auditOutcome(), statusTitle(), TestAuditWords(), TimeAt()

### Community 114 - "Dump (small)"
Cohesion: 0.33
Nodes (3): ParseDump(), TestParseDump(), Peer

### Community 115 - "2026 09 30 Netis Tags (small)"
Cohesion: 0.53
Nodes (6): A4 device form suggestions (datalists, Use detected), Tags redesign (T-series) plan, Tag palette (slate..sand, contrast-checked), TagChip / TagSpan templ components, views.TagColor (FNV-1a palette resolver), tags.js chip editor

### Community 116 - "2026 09 28 Netis Discovery Design (small)"
Cohesion: 0.53
Nodes (6): Discovery Improvements (F3) Design, Presence Without ICMP (ARP + TCP fallback), presence_fallback Setting, ResolveName (rDNS + mDNS PTR), SetIfaceHostnameIfEmpty, TCP Connect Confirmation Probe

### Community 117 - "2026 07 12 Netis Device Model Fields Design (small)"
Cohesion: 0.40
Nodes (4): Transactional Migrate Loop (FK-off rebuild), Richer Device Model Design (C1), Router & Modem Device Kinds, Scanning UX Design (C2)

### Community 118 - "Arp (small)"
Cohesion: 0.40
Nodes (4): ParseARPTable(), ReadARPTable(), TestParseARPTable(), TestParseARPTableNeedsComplete()

### Community 120 - "Time (small)"
Cohesion: 0.83
Nodes (3): rel(), span(), update()

### Community 121 - "Toasts (small)"
Cohesion: 0.83
Nodes (3): arm(), flashed(), init()

## Knowledge Gaps
- **74 isolated node(s):** `SQLite Store with Embedded Migrations`, `Session Auth and First-run Setup`, `templ + HTMX + SSE Stack`, `Needs-Attention Panel`, `Token-based Light/Dark Design System` (+69 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 335 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **124 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Store` connect `Startup Wiring & Sync Tests` to `Main Entrypoint & Backups`, `Web Handler Tests`, `Store Tests & Backup Runs`, `Healthcheck, OIDC & Config Tests`, `DHCP Lease Clients`, `API Token Auth Tests`, `Autofill Probe Tests`, `Rate Limiting & Proxy Trust`, `Notifications`, `Pi-hole Client`, `Scan Engine Events`, `Scan Engine Tests`, `Tag & Audit Handler Tests`, `Routing & DHCP Pool Parsing`, `Autofill Service`, `Notifier Tests`, `Upstream Reconciliation`, `Secret Encryption`, `Proxmox Client & Sync`, `OPNsense Integration`, `Scan Scheduler`, `Scan & Onboarding Tests`, `Scheduler Tests`, `Pi-hole Sync Tests`, `Database Migration Tool`, `E2E Fixture Test (small)`?**
  _High betweenness centrality (0.020) - this node is a cross-community bridge._
- **Why does `testServer()` connect `Web Handler Tests` to `Main Entrypoint & Backups`, `Store Tests & Backup Runs`, `Startup Wiring & Sync Tests`, `Healthcheck, OIDC & Config Tests`, `Admin Route Tests`, `API Token Auth Tests`, `E2E Fixture Test (small)`, `Rate Limiting & Proxy Trust`, `Tag & Audit Handler Tests`, `Server Test (small)`?**
  _High betweenness centrality (0.014) - this node is a cross-community bridge._
- **Why does `IsAdmin()` connect `Device Detail Views` to `Main Entrypoint & Backups`, `Dashboard Views`, `Scan Engine & Device Store`, `Subnet Grid & Free Ranges`, `Settings Templates`, `Device Filters & Private MACs`, `Shared Templ Components`, `Device List Views`?**
  _High betweenness centrality (0.009) - this node is a cross-community bridge._
- **Are the 276 inferred relationships involving `testServer()` (e.g. with `TestAboutTabReportsBuildAndRuntime()` and `TestAboutTabRequiresAuth()`) actually correct?**
  _`testServer()` has 276 INFERRED edges - model-reasoned connections that need verification._
- **Are the 163 inferred relationships involving `authedGet()` (e.g. with `TestAboutTabReportsBuildAndRuntime()` and `TestFooterLinksToAbout()`) actually correct?**
  _`authedGet()` has 163 INFERRED edges - model-reasoned connections that need verification._
- **What connects `SQLite Store with Embedded Migrations`, `Session Auth and First-run Setup`, `templ + HTMX + SSE Stack` to the rest of the system?**
  _74 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Web Handler Tests` be split into smaller, more focused modules?**
  _Cohesion score 0.02269595176571921 - nodes in this community are weakly interconnected._