# Device List Overhaul (Sub-project B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the devices page into a sortable, dual-view (list/grid) table with static/DHCP chips, device icons, and a reviewed/"new" marker plus an Approve action.

**Architecture:** Server-rendered (templ + the existing `?q` HTMX filter). Sorting is server-side (numeric IP order needs Go); the list/grid toggle is client-side, persisted in localStorage. One new device flag (`reviewed`, migration `0004`); the store's device row now carries the per-IP lease kind it already loads. Reuses sub-project A's components (`.pill`, `.chip`, `.seg`, `.ic`, `views.KindIcon`).

**Tech Stack:** Go 1.26, `net/netip`, templ + HTMX, `modernc.org/sqlite`, a small vanilla `devices.js`.

**Spec:** `docs/superpowers/specs/2026-07-12-netis-device-list-design.md`.

## Global Constraints

- Go module `netis`, `go1.26.5`, `CGO_ENABLED=0`. Build `CGO_ENABLED=0 go build ./...`; test `go test ./... -count=1`.
- **templ:** CLI at `/home/ben/go/bin/templ` (v0.3.1020; run `export PATH="$PATH:$(go env GOPATH)/bin"` first). After editing any `.templ`, run `templ generate` then build; commit BOTH the `.templ` and generated `*_templ.go`. Editing `.css`/`.js` needs no generate.
- `device.reviewed` is `1` for `source != 'scan'`, `0` for scan-discovered. `CreateDevice` derives it from source (ignores any passed value). `UpdateDevice` and Approve set it to `1`.
- Sort: `?sort ∈ {ip,name,status,kind,seen}` default `ip`; `?dir ∈ {asc,desc}` default `asc`; unknown values fall back to the defaults. IP sort is numeric by each device's lowest IP (`netip`), no-IP devices last.
- Lease kind values are `static`/`dhcp` (the `ip_assignment.kind` enum).
- View toggle persists in `localStorage["netis-devices-view"]`, default `list`.
- `errors.Is(err, sql.ErrNoRows)` for not-found lookups (project convention).
- Commit after each task; conventional-commit, body ending with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Shell prints harmless zsh-rc noise on stderr (`command not found: z`); ignore — exit codes are correct.

---

### Task 1: Store — `reviewed` flag, per-IP lease, migration `0004`

**Files:**
- Create: `internal/store/migrations/0004_device_reviewed.sql`
- Modify: `internal/store/device.go`, `internal/web/devices.go` (compile fix), `internal/web/views/devices.templ` (compile fix)
- Test: `internal/store/device_test.go`

**Interfaces:**
- Consumes: existing store types; `openTest`, `strp`.
- Produces:
  - `Device` gains `Reviewed bool`.
  - `type IPInfo struct { IP, Kind string }`; `DeviceRow.IPs` is now `[]IPInfo`.
  - `func (s *Store) SetDeviceReviewed(id int64, reviewed bool) error`.
  - `CreateDevice` sets `reviewed = (d.Source != "scan")`; `UpdateDevice` sets `reviewed=1`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/device_test.go`:

```go
func TestDeviceReviewedFromSource(t *testing.T) {
	s := openTest(t)
	scanID, _ := s.CreateDevice(Device{Name: "unknown-x", Kind: "other", Source: "scan"})
	manID, _ := s.CreateDevice(Device{Name: "nas", Kind: "server", Source: "manual"})
	scan, _ := s.GetDevice(scanID)
	man, _ := s.GetDevice(manID)
	if scan.Reviewed {
		t.Error("scan device should start unreviewed")
	}
	if !man.Reviewed {
		t.Error("manual device should start reviewed")
	}
}

func TestSetDeviceReviewedAndUpdate(t *testing.T) {
	s := openTest(t)
	id, _ := s.CreateDevice(Device{Name: "u", Kind: "other", Source: "scan"})
	if err := s.SetDeviceReviewed(id, true); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDevice(id)
	if !d.Reviewed {
		t.Fatal("SetDeviceReviewed(true) did not persist")
	}
	// A fresh scan device becomes reviewed when edited.
	id2, _ := s.CreateDevice(Device{Name: "u2", Kind: "other", Source: "scan"})
	d2, _ := s.GetDevice(id2)
	d2.Name = "edited"
	if err := s.UpdateDevice(d2); err != nil {
		t.Fatal(err)
	}
	d2, _ = s.GetDevice(id2)
	if !d2.Reviewed {
		t.Fatal("UpdateDevice should set reviewed=1")
	}
}

func TestListDevicesCarriesLeaseKindAndReviewed(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "nas", Kind: "server", Source: "manual"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:01"), nil)
	s.AssignIP(ifID, snID, "10.0.0.5", "static")
	s.AssignIP(ifID, snID, "10.0.0.6", "dhcp")

	rows, _ := s.ListDevices()
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	if !rows[0].Reviewed {
		t.Error("manual device row should be reviewed")
	}
	kinds := map[string]string{}
	for _, ip := range rows[0].IPs {
		kinds[ip.IP] = ip.Kind
	}
	if kinds["10.0.0.5"] != "static" || kinds["10.0.0.6"] != "dhcp" {
		t.Fatalf("lease kinds wrong: %+v", rows[0].IPs)
	}
}

func TestMigration0004Backfill(t *testing.T) {
	path := t.TempDir() + "/mig.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.Exec(`PRAGMA foreign_keys = ON;`)
	// Apply migrations 0001..0003 only, then insert rows without a reviewed column.
	for _, name := range []string{"0001_init.sql", "0002_pihole_source.sql", "0003_integration_status.sql"} {
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`)
	for _, name := range []string{"0001_init.sql", "0002_pihole_source.sql", "0003_integration_status.sql"} {
		db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name)
	}
	db.Exec(`INSERT INTO device (id,name,kind,source) VALUES (1,'unknown-x','other','scan')`)
	db.Exec(`INSERT INTO device (id,name,kind,source) VALUES (2,'nas','server','manual')`)
	db.Close()

	s, err := Open(path) // runs 0004 against the populated table
	if err != nil {
		t.Fatalf("Open (runs 0004): %v", err)
	}
	defer s.Close()
	scan, _ := s.GetDevice(1)
	man, _ := s.GetDevice(2)
	if scan.Reviewed {
		t.Error("backfill: scan device must stay unreviewed")
	}
	if !man.Reviewed {
		t.Error("backfill: manual device must become reviewed")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/store/ -run 'Reviewed|Migration0004|LeaseKind' -count=1`
Expected: FAIL — `Device` has no `Reviewed`, `IPInfo` undefined, `SetDeviceReviewed` undefined.

- [ ] **Step 3: Write the migration**

`internal/store/migrations/0004_device_reviewed.sql`:

```sql
ALTER TABLE device ADD COLUMN reviewed INTEGER NOT NULL DEFAULT 0;
UPDATE device SET reviewed = 1 WHERE source != 'scan';
```

- [ ] **Step 4: Update `device.go`**

Add `Reviewed` to the `Device` struct (after `Icon`):

```go
type Device struct {
	ID             int64
	Name           string
	Kind           string
	Notes          string
	Vendor         string
	Source         string
	ParentDeviceID *int64
	ProxmoxVMID    *int64
	WGPubKey       *string
	Icon           string
	Reviewed       bool
}
```

Add `IPInfo` and change `DeviceRow.IPs`:

```go
type IPInfo struct {
	IP   string
	Kind string
}

type DeviceRow struct {
	Device
	IPs      []IPInfo
	MACs     []string
	Online   bool
	LastSeen *string
	TagNames []string
}
```

Extend `deviceCols`, `scanDevice`, `CreateDevice`, `UpdateDevice`, and add `SetDeviceReviewed`:

```go
const deviceCols = `id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Notes, &d.Vendor, &d.Source,
		&d.ParentDeviceID, &d.ProxmoxVMID, &d.WGPubKey, &d.Icon, &d.Reviewed)
	return d, err
}

func (s *Store) CreateDevice(d Device) (int64, error) {
	// Scan-discovered devices start unreviewed; anything from a named source
	// (manual/proxmox/wireguard/pihole) is reviewed on creation.
	reviewed := d.Source != "scan"
	res, err := s.DB.Exec(`INSERT INTO device (name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source, d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon, reviewed)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateDevice(d Device) error {
	// Editing a device counts as reviewing it.
	_, err := s.DB.Exec(`UPDATE device SET name=?,kind=?,notes=?,vendor=?,source=?,
		parent_device_id=?,proxmox_vmid=?,wg_pubkey=?,icon=?,reviewed=1 WHERE id=?`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source,
		d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon, d.ID)
	return err
}

func (s *Store) SetDeviceReviewed(id int64, reviewed bool) error {
	_, err := s.DB.Exec(`UPDATE device SET reviewed=? WHERE id=?`, reviewed, id)
	return err
}
```

In `ListDevices`, change the IP-collection line to keep the kind:

```go
			for _, p := range ips {
				out[i].IPs = append(out[i].IPs, IPInfo{IP: p.IP, Kind: p.Kind})
			}
```

- [ ] **Step 5: Fix the two `DeviceRow.IPs` consumers so the build compiles**

In `internal/web/devices.go`, `handleDeviceList`'s filter builds the haystack from `row.IPs`; update the join to use `ip.IP`:

```go
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q != "" {
		filtered := rows[:0]
		for _, row := range rows {
			var ips []string
			for _, ip := range row.IPs {
				ips = append(ips, ip.IP)
			}
			hay := strings.ToLower(row.Name + " " + strings.Join(ips, " ") + " " +
				strings.Join(row.MACs, " ") + " " + strings.Join(row.TagNames, " "))
			if strings.Contains(hay, q) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
```

In `internal/web/views/devices.templ`, the `DeviceList` IP cell currently ranges `row.IPs` rendering `{ ip }`; change it to render `{ ip.IP }` (Task 2 replaces this markup entirely — this is only to compile):

```templ
					<td class="mono">
						for i, ip := range row.IPs {
							if i > 0 {
								{ ", " }
							}
							{ ip.IP }
						}
					</td>
```

- [ ] **Step 6: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/store/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/ internal/web/devices.go internal/web/views/devices.templ internal/web/views/devices_templ.go
git commit -m "feat(store): device reviewed flag, per-IP lease kind, migration 0004"
```

---

### Task 2: Sortable list + grid views

**Files:**
- Modify: `internal/web/devices.go` (sort), `internal/web/views/devices.templ` (`DeviceList` rewrite), `internal/web/static/app.css` (tiles + caret)
- Create: `internal/web/static/devices.js`
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Consumes: `DeviceRow` (`IPs []store.IPInfo`, `Reviewed`, `Online`, `LastSeen`, `TagNames`, `Kind`, `Name`), `views.KindIcon`, the `.pill/.chip/.seg/.ic` CSS.
- Produces: `views.DeviceList(username string, rows []store.DeviceRow, q, sort, dir string)`; the sorted handler; `devices.js` (view toggle); tile/caret CSS.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceListDefaultSortIPNumeric(t *testing.T) {
	srv, st := testServer(t)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	mk := func(name, ip string) {
		d, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		f, _ := st.AddIface(d, nil, nil)
		st.AssignIP(f, snID, ip, "dhcp")
	}
	mk("c", "10.0.0.100")
	mk("a", "10.0.0.2")
	mk("b", "10.0.0.10")
	body := authedGet(t, srv, st, "/devices").Body.String()
	// Numeric IP order: .2 before .10 before .100 (string sort would flip .10/.100/.2).
	i2, i10, i100 := strings.Index(body, "10.0.0.2<"), strings.Index(body, "10.0.0.10<"), strings.Index(body, "10.0.0.100<")
	if !(i2 >= 0 && i10 > i2 && i100 > i10) {
		t.Fatalf("IP order wrong: .2@%d .10@%d .100@%d", i2, i10, i100)
	}
}

func TestDeviceListLeaseChipsAndGrid(t *testing.T) {
	srv, st := testServer(t)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	d, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	f, _ := st.AddIface(d, nil, nil)
	st.AssignIP(f, snID, "10.0.0.5", "static")
	st.AssignIP(f, snID, "10.0.0.6", "dhcp")
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{"chip static", "chip", `id="dev-grid"`, `id="dev-list"`, `class="seg"`} {
		if !strings.Contains(body, want) {
			t.Errorf("device list missing %q", want)
		}
	}
}

func TestDeviceListNewMarker(t *testing.T) {
	srv, st := testServer(t)
	st.CreateDevice(store.Device{Name: "unknown-aa", Kind: "other", Source: "scan"})
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "new") {
		t.Fatal("unreviewed scan device should show a 'new' marker")
	}
}
```

(The tests assume `IP<` boundaries — the template renders each IP as `<span class="mono">10.0.0.2</span>`, so `10.0.0.2<` matches the closing tag. Adjust the sentinel if the markup differs, keeping the numeric-order assertion.)

- [ ] **Step 2: Run tests, verify failure**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -run 'DeviceListDefaultSort|LeaseChips|NewMarker' -count=1`
Expected: FAIL — no sort, no chips/grid/seg markup.

- [ ] **Step 3: Sort in the handler**

Replace `handleDeviceList` in `internal/web/devices.go` (keep the filter from Task 1, add sorting). Add imports `"net/netip"` and `"sort"`:

```go
func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q != "" {
		filtered := rows[:0]
		for _, row := range rows {
			var ips []string
			for _, ip := range row.IPs {
				ips = append(ips, ip.IP)
			}
			hay := strings.ToLower(row.Name + " " + strings.Join(ips, " ") + " " +
				strings.Join(row.MACs, " ") + " " + strings.Join(row.TagNames, " "))
			if strings.Contains(hay, q) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	sortKey := r.URL.Query().Get("sort")
	switch sortKey {
	case "ip", "name", "status", "kind", "seen":
	default:
		sortKey = "ip"
	}
	dir := r.URL.Query().Get("dir")
	if dir != "desc" {
		dir = "asc"
	}
	sortDeviceRows(rows, sortKey, dir)

	u, _ := userFrom(r)
	views.DeviceList(u.Username, rows, r.URL.Query().Get("q"), sortKey, dir).Render(r.Context(), w)
}

// lowestIP returns the device's numerically smallest IP, or the zero Addr
// (which sorts before all real addresses) when it has none; callers push
// no-IP devices to the end explicitly.
func lowestIP(row store.DeviceRow) (netip.Addr, bool) {
	var best netip.Addr
	found := false
	for _, ip := range row.IPs {
		a, err := netip.ParseAddr(ip.IP)
		if err != nil {
			continue
		}
		if !found || a.Compare(best) < 0 {
			best, found = a, true
		}
	}
	return best, found
}

func sortDeviceRows(rows []store.DeviceRow, key, dir string) {
	less := func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch key {
		case "name":
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case "kind":
			return a.Kind < b.Kind
		case "status":
			return a.Online && !b.Online // online first
		case "seen":
			as, bs := "", ""
			if a.LastSeen != nil {
				as = *a.LastSeen
			}
			if b.LastSeen != nil {
				bs = *b.LastSeen
			}
			return as > bs // most-recent first; never-seen ("") last
		default: // ip
			ai, aok := lowestIP(a)
			bi, bok := lowestIP(b)
			if aok != bok {
				return aok // devices with an IP sort before those without
			}
			if !aok {
				return false
			}
			return ai.Compare(bi) < 0
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if dir == "desc" {
			return less(j, i)
		}
		return less(i, j)
	})
}
```

- [ ] **Step 4: Rewrite `DeviceList` and add `sortHeader`**

Replace the `DeviceList` templ (and add a `sortHeader` helper) in `internal/web/views/devices.templ`. Add `"net/url"` to the file's import block (alongside `fmt` and `store`). The new `DeviceList`:

```templ
templ sortHeader(label, col, curSort, curDir, q string) {
	<th>
		<a href={ templ.URL(sortURL(q, col, curSort, curDir)) }>
			{ label }
			if curSort == col {
				if curDir == "asc" {
					{ " ▲" }
				} else {
					{ " ▼" }
				}
			}
		</a>
	</th>
}

templ DeviceList(username string, rows []store.DeviceRow, q, sortKey, dir string) {
	@Layout("Devices", username) {
		<h1>Devices</h1>
		<div class="toolbar">
			<input
				class="grow" type="search" name="q" value={ q } placeholder="filter devices, IP, MAC, tag…"
				hx-get="/devices" hx-trigger="input changed delay:300ms" hx-target="body" hx-push-url="true"
			/>
			<div class="seg" id="dev-view">
				<button type="button" data-view="list">List</button>
				<button type="button" data-view="grid">Grid</button>
			</div>
			<a href="/devices/new"><button type="button" class="primary">New device</button></a>
		</div>

		<div id="dev-list">
			<table>
				<thead>
					<tr>
						@sortHeader("Device", "name", sortKey, dir, q)
						@sortHeader("IP", "ip", sortKey, dir, q)
						<th>Lease</th>
						@sortHeader("Status", "status", sortKey, dir, q)
						@sortHeader("Kind", "kind", sortKey, dir, q)
						<th>Tags</th>
						@sortHeader("Last seen", "seen", sortKey, dir, q)
					</tr>
				</thead>
				<tbody>
					for _, row := range rows {
						<tr>
							<td>
								<div class="dev">
									<span class="ic">{ KindIcon(row.Kind) }</span>
									<div>
										<div>
											<a href={ templ.URL(fmt.Sprintf("/devices/%d", row.ID)) }>{ row.Name }</a>
											if row.Reviewed {
												<span class="muted" title="reviewed">✓</span>
											} else {
												<span class="pill reserved"><span class="d"></span>new</span>
											}
										</div>
										for i, mac := range row.MACs {
											if i == 0 {
												<div class="mono muted" style="font-size:11.5px">{ mac }</div>
											}
										}
									</div>
								</div>
							</td>
							<td class="mono">
								for _, ip := range row.IPs {
									<span>{ ip.IP }</span>{ " " }
								}
							</td>
							<td>
								for _, ip := range row.IPs {
									if ip.Kind == "static" {
										<span class="chip static">static</span>{ " " }
									} else {
										<span class="chip">dhcp</span>{ " " }
									}
								}
							</td>
							<td>
								if row.Online {
									<span class="pill online"><span class="d"></span>online</span>
								} else {
									<span class="pill offline"><span class="d"></span>offline</span>
								}
							</td>
							<td>{ row.Kind }</td>
							<td>
								for _, tn := range row.TagNames {
									<span class="tag">{ tn }</span>{ " " }
								}
							</td>
							<td class="mono muted">
								if row.LastSeen != nil {
									{ *row.LastSeen }
								}
							</td>
						</tr>
					}
				</tbody>
			</table>
		</div>

		<div id="dev-grid" class="devgrid" style="display:none">
			for _, row := range rows {
				<a class="devtile" href={ templ.URL(fmt.Sprintf("/devices/%d", row.ID)) }>
					<span class="st">
						if row.Online {
							<span class="pill online"><span class="d"></span></span>
						} else {
							<span class="pill offline"><span class="d"></span></span>
						}
					</span>
					<span class="ic">{ KindIcon(row.Kind) }</span>
					<div class="nm">
						{ row.Name }
						if !row.Reviewed {
							<span class="pill reserved" style="margin-left:4px"><span class="d"></span>new</span>
						}
					</div>
					<div class="ip mono">
						for i, ip := range row.IPs {
							if i == 0 {
								{ ip.IP }
							}
						}
					</div>
				</a>
			}
		</div>
		<script src="/static/devices.js"></script>
	}
}

func nextDir(col, curSort, curDir string) string {
	if curSort == col && curDir == "asc" {
		return "desc"
	}
	return "asc"
}

// sortURL builds a /devices link that preserves the filter (URL-encoded) and
// toggles the sort direction for the clicked column.
func sortURL(q, col, curSort, curDir string) string {
	return "/devices?q=" + url.QueryEscape(q) + "&sort=" + col + "&dir=" + nextDir(col, curSort, curDir)
}
```

(`url` is `net/url`; add it to the `devices.templ` import block.)

- [ ] **Step 5: Create `devices.js`**

`internal/web/static/devices.js`:

```js
(function () {
	function stored() {
		try { return localStorage.getItem('netis-devices-view') || 'list'; } catch (e) { return 'list'; }
	}
	function show(view) {
		var list = document.getElementById('dev-list');
		var grid = document.getElementById('dev-grid');
		if (!list || !grid) { return; }
		list.style.display = view === 'grid' ? 'none' : '';
		grid.style.display = view === 'grid' ? '' : 'none';
		document.querySelectorAll('#dev-view button').forEach(function (b) {
			b.classList.toggle('on', b.getAttribute('data-view') === view);
		});
	}
	show(stored());
	document.querySelectorAll('#dev-view button').forEach(function (b) {
		b.addEventListener('click', function () {
			var v = b.getAttribute('data-view');
			try { localStorage.setItem('netis-devices-view', v); } catch (e) {}
			show(v);
		});
	});
})();
```

- [ ] **Step 6: Add tile + toolbar CSS**

Append to `internal/web/static/app.css`:

```css
.toolbar { display:flex; align-items:center; gap:10px; margin-bottom:14px; }
.toolbar .grow { flex:1; }
.dev { display:flex; align-items:center; gap:10px; }
.dev a { text-decoration:none; font-weight:540; }
thead th a { color:inherit; text-decoration:none; }
thead th a:hover { color:var(--fg); }
.devgrid { display:grid; grid-template-columns:repeat(auto-fill,minmax(150px,1fr)); gap:12px; }
.devtile { position:relative; background:var(--surface); border:1px solid var(--border); border-radius:var(--radius);
           padding:13px; box-shadow:var(--shadow); color:inherit; text-decoration:none; display:block; }
.devtile:hover { border-color:var(--accent); }
.devtile .ic { width:38px; height:38px; font-size:19px; border-radius:10px; }
.devtile .nm { font-weight:580; margin-top:9px; }
.devtile .ip { color:var(--muted); font-size:12px; margin-top:1px; }
.devtile .st { position:absolute; top:12px; right:12px; }
```

- [ ] **Step 7: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web/devices.go internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/static/devices.js internal/web/static/app.css internal/web/devices_test.go
git commit -m "feat(web): sortable device list with grid view, lease chips, and icons"
```

---

### Task 3: Approve action

**Files:**
- Modify: `internal/web/devices.go` (approve handler), `internal/web/server.go` (route), `internal/web/views/devices.templ` (Approve button)
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Consumes: `store.SetDeviceReviewed` (Task 1); the list rendering (Task 2).
- Produces: `POST /devices/{id}/approve` (admin) → sets reviewed, 303 to `/devices`; an Approve button on unreviewed list rows.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestApproveDevice(t *testing.T) {
	srv, st := testServer(t)
	devID, _ := st.CreateDevice(store.Device{Name: "unknown-bb", Kind: "other", Source: "scan"})
	// The unreviewed device shows an Approve control.
	if !strings.Contains(authedGet(t, srv, st, "/devices").Body.String(), "/devices/1/approve") {
		t.Fatal("unreviewed device should show an Approve control")
	}
	rec := authedPost(t, srv, st, "/devices/1/approve", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("approve code=%d", rec.Code)
	}
	d, _ := st.GetDevice(devID)
	if !d.Reviewed {
		t.Fatal("approve did not set reviewed")
	}
	// After approval the Approve control is gone.
	if strings.Contains(authedGet(t, srv, st, "/devices").Body.String(), "/devices/1/approve") {
		t.Fatal("approved device should no longer show Approve")
	}
}

func TestApproveRequiresAdmin(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	st.CreateDevice(store.Device{Name: "unknown-cc", Kind: "other", Source: "scan"})
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/devices/1/approve", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer approve code=%d, want 403", rec.Code)
	}
}
```

(Ensure `"net/http"`, `"net/http/httptest"`, `"net/url"` are imported in the test file.)

- [ ] **Step 2: Run tests, verify failure**

Run: `templ generate && go test ./internal/web/ -run 'ApproveDevice|ApproveRequiresAdmin' -count=1`
Expected: FAIL — route 404, no Approve control.

- [ ] **Step 3: Add the handler + route**

In `internal/web/devices.go`, add:

```go
func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.SetDeviceReviewed(id, true); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices", http.StatusSeeOther)
}
```

In `internal/web/server.go`, register the route with the other `/devices/{id}/…` admin routes:

```go
	s.mux.HandleFunc("POST /devices/{id}/approve", s.requireAdmin(s.handleDeviceApprove))
```

- [ ] **Step 4: Add the Approve button to the list**

In `internal/web/views/devices.templ`, add a trailing actions column. In the `<thead>` row add `<th></th>` after the "Last seen" header, and in each `<tbody>` row add a final cell after the last-seen cell:

```templ
							<td>
								if !row.Reviewed {
									<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/approve", row.ID)) } class="inline">
										<button type="submit">Approve</button>
									</form>
								}
							</td>
```

- [ ] **Step 5: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/devices.go internal/web/server.go internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices_test.go
git commit -m "feat(web): approve action to mark auto-discovered devices reviewed"
```

---

## Final verification (after Task 3)

- [ ] `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && CGO_ENABLED=0 go test ./... -count=1` — all green.
- [ ] `CGO_ENABLED=0 go build -o netis ./cmd/netis` — builds.
- [ ] Manual visual smoke: run the binary, add a subnet + a few devices with static and dhcp IPs, plus let a scan create an unknown device. On `/devices`: default order is numeric-IP ascending; clicking a column header sorts + shows a caret; the list/grid toggle switches and persists across reloads; static/dhcp chips and device icons render; the unreviewed device shows a "new" marker + Approve, and approving clears it. Check both themes.
- [ ] Use superpowers:finishing-a-development-branch.
