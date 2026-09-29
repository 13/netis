# Device autofill A1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fill vendor, model, kind, icon, function and tags on devices from the full IEEE OUI registry, hostnames and open ports, never overwriting what a person set, and show where each value came from.

**Architecture:** Sources write *hints* (device, source, field, value, confidence) to a `device_hint` table. A pure resolver picks one candidate per field; pure apply rules compare it with the device and a `device_autofill` record of what netis wrote, and produce compare-and-swap writes the store applies in one transaction. An `autofill.Service` recomputes local hints and applies them on a coalesced full pass kicked after sweeps and integration runs.

**Tech Stack:** Go 1.2x, database/sql over SQLite and Postgres, templ, htmx. Spec: `docs/superpowers/specs/2026-09-29-netis-autofill-design.md`.

## Global Constraints

- Every migration exists under the same name in `internal/store/migrations/sqlite/` and `internal/store/migrations/postgres/`.
- Store tests run through `storetest.EachDialect` (or the store package's own dialect helper where the file already uses one).
- Autofill never overwrites a value a person typed, imported or accepted; a changed or removed value becomes `owned` and is never touched again.
- Autofill never fails a sweep or a lease sync: errors are logged.
- Hints below confidence 50 are stored and shown but not applied.
- Source rank for ties: `mdns > ssdp > ports > hostname > oui`.
- UI copy is sentence case, plain words (see the U8 copy pass); use "detected", not "autofilled".
- Run `make generate` before `go test` whenever a `.templ` file changed.
- Commit messages: conventional prefix (`feat(autofill):`, `feat(oui):`, `test:`, `docs:`), ending with the session's attribution lines.

## File map

- Create `internal/oui/oui.go` — embedded registry, `Vendor(mac)`, longest prefix.
- Create `internal/oui/normalize.go` — `Normalize(name)` for registry names.
- Create `internal/oui/gen/main.go` — fetches IEEE CSVs, writes `internal/oui/oui.txt.gz`.
- Create `internal/oui/oui.txt.gz` — generated data (committed).
- Delete `internal/scan/oui.go`, `internal/scan/oui_data.txt`, `internal/scan/oui_test.go`.
- Create `internal/store/migrations/{sqlite,postgres}/0016_autofill.sql`.
- Create `internal/store/autofill.go` — hint and record persistence, `ApplyAutofill`.
- Create `internal/autofill/resolve.go` — `Resolve`, `decide`, `Explain`.
- Create `internal/autofill/sources.go` — `ouiHints`, `hostnameHints`, `portHints`.
- Create `internal/autofill/service.go` — `Service` (`Run`, `Kick`, `Start`).
- Modify `internal/scan/engine.go` — drop vendor at create, kick after sweep.
- Modify `cmd/netis/main.go` — build and start the service, kick after integration runs.
- Modify `internal/web/server.go`, `devices.go`, `apiwrite.go`, `settings.go` — options, port scan, create, toggle.
- Modify `internal/web/views/device_detail.templ`, `settings_network.templ` — badges, detected panel, toggle.
- Modify `internal/web/testdata/e2e_seed.sql`, `docs/discovery.md`.

---

### Task 1: IEEE OUI registry package

**Files:**
- Create: `internal/oui/normalize.go`, `internal/oui/oui.go`, `internal/oui/gen/main.go`, `internal/oui/oui.txt.gz`
- Test: `internal/oui/oui_test.go`, `internal/oui/normalize_test.go`
- Modify: `internal/scan/engine.go:219` (use `oui.Vendor`), delete `internal/scan/oui.go`, `internal/scan/oui_data.txt`, `internal/scan/oui_test.go`
- Modify: `Makefile` (add `oui` target)

**Interfaces:**
- Produces: `oui.Vendor(mac string) string` — short vendor name or `""`; `oui.Normalize(name string) string`.

- [ ] **Step 1: Write the failing normalize test**

`internal/oui/normalize_test.go`:

```go
package oui

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Apple, Inc.":                      "Apple",
		"TP-LINK TECHNOLOGIES CO.,LTD.":    "TP-Link",
		"Samsung Electronics Co.,Ltd":      "Samsung",
		"Raspberry Pi Trading Ltd":         "Raspberry Pi",
		"Raspberry Pi Foundation":          "Raspberry Pi",
		"Proxmox Server Solutions GmbH":    "Proxmox Server Solutions",
		"Espressif Inc.":                   "Espressif",
		"Synology Incorporated":            "Synology",
		"ASUSTek COMPUTER INC.":            "ASUS",
		"Hon Hai Precision Ind. Co.,Ltd.":  "Foxconn",
		"Seiko Epson Corporation":          "Epson",
		"Brother Industries, LTD.":         "Brother",
		"Ubiquiti Inc":                     "Ubiquiti",
		"  Sony   Corporation ":            "Sony",
		"NORDIC SEMICONDUCTOR ASA":         "Nordic Semiconductor ASA",
		"Electronics Ltd":                  "Electronics",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/oui/`
Expected: FAIL, `undefined: Normalize`.

- [ ] **Step 3: Implement `Normalize`**

`internal/oui/normalize.go`:

```go
package oui

import (
	"regexp"
	"strings"
)

// suffixRe matches one trailing legal-form or filler word. Normalize strips
// it repeatedly, so "TP-LINK TECHNOLOGIES CO.,LTD." loses ",LTD.", then
// " CO.", then " TECHNOLOGIES".
var suffixRe = regexp.MustCompile(`(?i)[\s,.]+(inc|incorporated|corp|corporate|corporation|co|company|ltd|limited|llc|l\.l\.c|gmbh|ag|s\.?a|s\.?p\.?a|s\.?a\.?s|b\.?v|n\.?v|oy|ab|a/s|kg|plc|pty|pte|s\.?r\.?l|k\.?k|technologies|technology|electronics|international|ind)\.?$`)

var spaceRe = regexp.MustCompile(`\s+`)

// overrides maps a stripped, lower-cased registry name to the name people
// know the maker by.
var overrides = map[string]string{
	"raspberry pi trading":       "Raspberry Pi",
	"raspberry pi foundation":    "Raspberry Pi",
	"hon hai precision":          "Foxconn",
	"hon hai precision industry": "Foxconn",
	"tp-link":                    "TP-Link",
	"tp-link systems":            "TP-Link",
	"asustek computer":           "ASUS",
	"intel":                      "Intel",
	"hewlett packard":            "HP",
	"hewlett packard enterprise": "HPE",
	"ubiquiti networks":          "Ubiquiti",
	"xiaomi communications":      "Xiaomi",
	"seiko epson":                "Epson",
	"brother industries":         "Brother",
	"murata manufacturing":       "Murata",
	"allterco robotics eood":     "Shelly",
	"avm audiovisuelles marketing und computersysteme": "AVM",
}

// Normalize turns an IEEE registry organisation name into a short vendor
// name: whitespace collapsed, legal suffixes stripped (never down to
// nothing), well-known makers renamed, and ALL-CAPS names title-cased.
func Normalize(name string) string {
	s := strings.TrimSpace(spaceRe.ReplaceAllString(name, " "))
	for {
		loc := suffixRe.FindStringIndex(s)
		if loc == nil || loc[0] == 0 {
			break
		}
		s = strings.TrimRight(s[:loc[0]], " ,.")
	}
	if o, ok := overrides[strings.ToLower(s)]; ok {
		return o
	}
	if s != strings.ToUpper(s) || len(s) <= 4 {
		return s
	}
	words := strings.Split(s, " ")
	for i, w := range words {
		if len(w) > 3 {
			words[i] = w[:1] + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}
```

Note: `NORDIC SEMICONDUCTOR ASA` title-cases to `Nordic Semiconductor ASA` (`ASA` is 3 letters, kept). If a case in the test fails because the regexp strips too much or too little, fix the regexp or the override, not the test.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/oui/ -run TestNormalize -v`
Expected: PASS.

- [ ] **Step 5: Write the generator**

`internal/oui/gen/main.go`:

```go
// Command gen rebuilds internal/oui/oui.txt.gz from the IEEE MA-L, MA-M and
// MA-S registries. Run from the repository root: go run ./internal/oui/gen
package main

import (
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"netis/internal/oui"
)

var registries = []string{
	"https://standards-oui.ieee.org/oui/oui.csv",
	"https://standards-oui.ieee.org/oui28/mam.csv",
	"https://standards-oui.ieee.org/oui36/oui36.csv",
}

func main() {
	out := flag.String("o", "internal/oui/oui.txt.gz", "output file")
	flag.Parse()
	entries := map[string]string{}
	client := &http.Client{Timeout: 2 * time.Minute}
	for _, u := range registries {
		if err := fetch(client, u, entries); err != nil {
			log.Fatalf("%s: %v", u, err)
		}
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	zw.ModTime = time.Time{} // reproducible output
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, entries[k])
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d prefixes to %s", len(keys), *out)
}

// fetch reads one registry CSV (Registry,Assignment,Organization Name,
// Organization Address) into entries, keyed by upper-case hex assignment.
func fetch(client *http.Client, url string, entries map[string]string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "netis-oui-gen (+https://github.com/)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %s", resp.Status)
	}
	r := csv.NewReader(resp.Body)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if _, err := r.Read(); err != nil { // header
		return err
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(rec) < 3 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(rec[1]))
		name := oui.Normalize(rec[2])
		if key == "" || name == "" || strings.EqualFold(name, "private") {
			continue
		}
		entries[key] = name
	}
}
```

Add to `Makefile` (and to its `.PHONY` line):

```make
# Refreshes the embedded IEEE OUI registry (internal/oui/oui.txt.gz).
oui:
	go run ./internal/oui/gen
```

- [ ] **Step 6: Generate the data**

Run: `go run ./internal/oui/gen`
Expected: `wrote N prefixes to internal/oui/oui.txt.gz` with N above 40000, file under 1.5 MB (`ls -l internal/oui/oui.txt.gz`).
Spot check: `zcat internal/oui/oui.txt.gz | grep -E '^(BC2411|B827EB|3C0754|EC086B)'` shows Proxmox Server Solutions, Raspberry Pi, Apple, TP-Link. If a common maker comes out ugly (e.g. `zcat … | cut -f2 | sort | uniq -c | sort -rn | head -60`), add an override and a test case, then regenerate.

- [ ] **Step 7: Write the failing lookup test**

`internal/oui/oui_test.go`:

```go
package oui

import "testing"

func TestLookupLongestPrefix(t *testing.T) {
	table := map[string]string{"AABBCC": "Large", "AABBCC1": "Medium", "AABBCC123": "Small"}
	cases := map[string]string{
		"aa:bb:cc:00:00:01": "Large",
		"aa:bb:cc:10:00:01": "Medium",
		"AA-BB-CC-12-34-56": "Small",
		"aabbcc123fff":      "Small",
		"dd:ee:ff:00:00:00": "",
		"":                  "",
		"zz":                "",
	}
	for mac, want := range cases {
		if got := lookup(table, mac); got != want {
			t.Errorf("lookup(%q) = %q, want %q", mac, got, want)
		}
	}
}

func TestVendorRealRegistry(t *testing.T) {
	cases := map[string]string{
		"bc:24:11:aa:bb:cc": "Proxmox Server Solutions",
		"b8:27:eb:00:00:01": "Raspberry Pi",
		"3c:07:54:00:00:01": "Apple",
		"52:54:00:12:34:56": "QEMU",
		"00:15:5d:00:00:01": "Microsoft Hyper-V",
		// Locally administered (randomized) MACs have no vendor.
		"da:a1:19:00:00:01": "",
	}
	for mac, want := range cases {
		if got := Vendor(mac); got != want {
			t.Errorf("Vendor(%q) = %q, want %q", mac, got, want)
		}
	}
}
```

- [ ] **Step 8: Run to verify it fails**

Run: `go test ./internal/oui/`
Expected: FAIL, `undefined: lookup`, `undefined: Vendor`.

- [ ] **Step 9: Implement the lookup**

`internal/oui/oui.go`:

```go
// Package oui names the maker of a network interface from its MAC address,
// using the IEEE MA-L, MA-M and MA-S registries (refresh with make oui).
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"log/slog"
	"strings"
	"sync"

	"netis/internal/macaddr"
)

//go:embed oui.txt.gz
var registryGz []byte

// extras names prefixes the IEEE registry does not: virtual NICs whose
// locally administered prefixes are stable (see macaddr.IsPrivate).
var extras = map[string]string{
	"525400": "QEMU",
	"00155D": "Microsoft Hyper-V",
}

var (
	once     sync.Once
	registry map[string]string
)

func load() {
	registry = make(map[string]string, 60000)
	zr, err := gzip.NewReader(bytes.NewReader(registryGz))
	if err != nil {
		slog.Error("oui registry unreadable", "err", err)
		return
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "\t")
		if ok {
			registry[k] = v
		}
	}
	for k, v := range extras {
		registry[k] = v
	}
}

// Vendor returns the short name of the maker the MAC's prefix is registered
// to, or "" when it is unknown or the MAC is randomized.
func Vendor(mac string) string {
	once.Do(load)
	if macaddr.IsPrivate(mac) {
		return ""
	}
	return lookup(registry, mac)
}

// lookup finds the longest registered prefix of mac in table: 36 bits
// (MA-S), then 28 (MA-M), then 24 (MA-L).
func lookup(table map[string]string, mac string) string {
	hex := strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "").Replace(mac))
	for _, n := range []int{9, 7, 6} {
		if len(hex) < n {
			continue
		}
		if v, ok := table[hex[:n]]; ok {
			return v
		}
	}
	return ""
}
```

Check `macaddr.IsPrivate("00:15:5d:…")` is false and `IsPrivate("52:54:00:…")` is false (stable prefixes); if `IsPrivate` treats 52:54:00 as private, check `extras` before the private test.

- [ ] **Step 10: Switch the scan engine and delete the old table**

In `internal/scan/engine.go` `createUnknown`, replace `Vendor(mac)` with `oui.Vendor(mac)` and import `netis/internal/oui` (Task 5 removes this call). Delete `internal/scan/oui.go`, `internal/scan/oui_data.txt`, `internal/scan/oui_test.go`. In `internal/scan/engine_test.go` `TestAutoCreatesUnknownDevice`, change the expected vendor to `"Proxmox Server Solutions"`.

- [ ] **Step 11: Run the tests**

Run: `go test ./internal/oui/ ./internal/scan/`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add Makefile internal/oui internal/scan
git commit -m "feat(oui): name makers from the full IEEE registry"
```

---

### Task 2: Hint and autofill tables, store API

**Files:**
- Create: `internal/store/migrations/sqlite/0016_autofill.sql`, `internal/store/migrations/postgres/0016_autofill.sql`, `internal/store/autofill.go`
- Test: `internal/store/autofill_test.go`

**Interfaces:**
- Produces (package `store`):

```go
type Hint struct {
	DeviceID   int64
	Source     string // oui, hostname, ports, mdns, ssdp
	Field      string // vendor, model, kind, icon, function, name, tag
	Value      string
	Confidence int
	Detail     string
	SeenAt     string
}
const (AutofillApplied = "applied"; AutofillOwned = "owned")
type AutofillRecord struct {
	DeviceID  int64
	Field     string // vendor, model, kind, icon, function, name, or tag:<name>
	Value     string
	Source    string
	State     string
	UpdatedAt string
}
type AutofillWrite struct {
	Field, Value, Source string
	Expect     string // the value read before deciding; the write is skipped if it changed
	Unreviewed bool   // also require the device still to be unreviewed
}
type AutofillTag struct{ Name, Source string }
type AutofillChanges struct {
	Writes []AutofillWrite
	Tags   []AutofillTag
	Own    []string // record fields (vendor, tag:nas, …) the person now owns
}
func (c AutofillChanges) Empty() bool
func (s *Store) ReplaceHints(ctx context.Context, deviceID int64, source string, hints []Hint) error
func (s *Store) ListHints(ctx context.Context, deviceID int64) ([]Hint, error)
func (s *Store) ListAutofill(ctx context.Context, deviceID int64) ([]AutofillRecord, error)
func (s *Store) ApplyAutofill(ctx context.Context, deviceID int64, ch AutofillChanges) (writes []AutofillWrite, tags []AutofillTag, err error)
func (s *Store) DeviceIDs(ctx context.Context) ([]int64, error)
```

- [ ] **Step 1: Write the migrations**

`internal/store/migrations/sqlite/0016_autofill.sql` and `internal/store/migrations/postgres/0016_autofill.sql` (identical except `device_id` type: `INTEGER` in SQLite, `BIGINT` in Postgres):

```sql
-- Device autofill. device_hint holds what each source observed about a
-- device; a source replaces its own rows each time it runs. device_autofill
-- holds what autofill wrote to a device, so a later change by a person is
-- recognised (value differs) and the field becomes theirs (state owned).
-- field there is a device column name, or tag:<name> for a tag.
CREATE TABLE device_hint (
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  field TEXT NOT NULL,
  value TEXT NOT NULL,
  confidence INTEGER NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  seen_at TEXT NOT NULL,
  PRIMARY KEY (device_id, source, field, value)
);
CREATE TABLE device_autofill (
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  field TEXT NOT NULL,
  value TEXT NOT NULL,
  source TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('applied','owned')),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (device_id, field)
);
```

- [ ] **Step 2: Write the failing store test**

`internal/store/autofill_test.go` (package `store_test`, like other files that use `storetest`; if store's own tests use an in-package dialect helper instead, follow that file's pattern):

```go
package store_test

import (
	"testing"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

func TestHintsReplacePerSource(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		id, err := st.CreateDevice(ctx, store.Device{Name: "d", Kind: "other", Source: "scan"})
		if err != nil {
			t.Fatal(err)
		}
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(st.ReplaceHints(ctx, id, "oui", []store.Hint{{Field: "vendor", Value: "Apple", Confidence: 90, Detail: "MAC 3c:07:54:00:00:01", SeenAt: "2026-09-29T00:00:00Z"}}))
		must(st.ReplaceHints(ctx, id, "hostname", []store.Hint{{Field: "kind", Value: "phone", Confidence: 70, SeenAt: "2026-09-29T00:00:00Z"}}))
		must(st.ReplaceHints(ctx, id, "oui", nil)) // oui has nothing now
		hs, err := st.ListHints(ctx, id)
		must(err)
		if len(hs) != 1 || hs[0].Source != "hostname" || hs[0].Value != "phone" || hs[0].DeviceID != id {
			t.Fatalf("hints = %+v", hs)
		}
		must(st.DeleteDevice(ctx, id))
		hs, _ = st.ListHints(ctx, id)
		if len(hs) != 0 {
			t.Fatalf("hints survived device delete: %+v", hs)
		}
	})
}

func TestApplyAutofillCompareAndSwap(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		id, _ := st.CreateDevice(ctx, store.Device{Name: "unknown-aa", Kind: "other", Source: "scan"})
		writes, tags, err := st.ApplyAutofill(ctx, id, store.AutofillChanges{
			Writes: []store.AutofillWrite{
				{Field: "vendor", Value: "Brother", Source: "hostname", Expect: ""},
				{Field: "kind", Value: "printer", Source: "hostname", Expect: "other"},
				// Stale read: model is "" but we claim "X", so this write is skipped.
				{Field: "model", Value: "HL-L2350", Source: "hostname", Expect: "X"},
			},
			Tags: []store.AutofillTag{{Name: "office", Source: "hostname"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != 2 || len(tags) != 1 {
			t.Fatalf("applied writes=%+v tags=%+v", writes, tags)
		}
		d, _ := st.GetDevice(ctx, id)
		if d.Vendor != "Brother" || d.Kind != "printer" || d.Model != "" || d.Reviewed {
			t.Fatalf("device = %+v", d)
		}
		recs, _ := st.ListAutofill(ctx, id)
		got := map[string]store.AutofillRecord{}
		for _, r := range recs {
			got[r.Field] = r
		}
		if got["vendor"].Value != "Brother" || got["vendor"].State != store.AutofillApplied ||
			got["tag:office"].State != store.AutofillApplied || len(got) != 3 {
			t.Fatalf("records = %+v", recs)
		}

		// A person owns vendor now; Own flips the record.
		if _, _, err := st.ApplyAutofill(ctx, id, store.AutofillChanges{Own: []string{"vendor"}}); err != nil {
			t.Fatal(err)
		}
		recs, _ = st.ListAutofill(ctx, id)
		for _, r := range recs {
			if r.Field == "vendor" && r.State != store.AutofillOwned {
				t.Fatalf("vendor record = %+v", r)
			}
		}

		// Unreviewed guard: once reviewed, an Unreviewed write is skipped.
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(st.SetDeviceReviewed(ctx, id, true))
		writes, _, err = st.ApplyAutofill(ctx, id, store.AutofillChanges{Writes: []store.AutofillWrite{
			{Field: "name", Value: "Printer", Source: "mdns", Expect: "unknown-aa", Unreviewed: true},
		}})
		must(err)
		if len(writes) != 0 {
			t.Fatalf("wrote to a reviewed device: %+v", writes)
		}

		// An unknown field is an error, not SQL injection.
		if _, _, err := st.ApplyAutofill(ctx, id, store.AutofillChanges{Writes: []store.AutofillWrite{{Field: "reviewed", Value: "1"}}}); err == nil {
			t.Fatal("want error for unknown field")
		}
	})
}

func TestDeviceIDs(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		a, _ := st.CreateDevice(t.Context(), store.Device{Name: "a", Kind: "other", Source: "manual"})
		b, _ := st.CreateDevice(t.Context(), store.Device{Name: "b", Kind: "other", Source: "manual"})
		ids, err := st.DeviceIDs(t.Context())
		if err != nil || len(ids) != 2 || ids[0] != a || ids[1] != b {
			t.Fatalf("ids=%v err=%v", ids, err)
		}
	})
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/store/ -run 'Hints|ApplyAutofill|DeviceIDs'`
Expected: FAIL, undefined `ReplaceHints` etc.

- [ ] **Step 4: Implement `internal/store/autofill.go`**

```go
package store

import (
	"context"
	"fmt"
	"time"
)

// Hint is one source's observation that a device's field is probably value.
// See the autofill package for how hints become device values.
type Hint struct {
	DeviceID   int64
	Source     string
	Field      string
	Value      string
	Confidence int
	// Detail is the evidence, for people: "MAC 3c:07:54:…", "hostname BRW…".
	Detail string
	SeenAt string
}

// Autofill record states: applied while the device still holds the value
// autofill wrote; owned once a person changed or removed it.
const (
	AutofillApplied = "applied"
	AutofillOwned   = "owned"
)

// AutofillRecord is what autofill wrote to one field (or tag:<name>) of a
// device, and whether that value is still autofill's.
type AutofillRecord struct {
	DeviceID  int64
	Field     string
	Value     string
	Source    string
	State     string
	UpdatedAt string
}

// AutofillWrite sets one device column, but only if it still holds Expect
// (and, with Unreviewed, only while the device is unreviewed), so a person's
// edit between the read and the write always wins.
type AutofillWrite struct {
	Field, Value, Source string
	Expect               string
	Unreviewed           bool
}

// AutofillTag attaches a tag the device does not have.
type AutofillTag struct{ Name, Source string }

// AutofillChanges is everything one autofill pass wants to do to a device.
type AutofillChanges struct {
	Writes []AutofillWrite
	Tags   []AutofillTag
	// Own lists record fields a person has taken over.
	Own []string
}

// Empty reports whether there is nothing to apply.
func (c AutofillChanges) Empty() bool {
	return len(c.Writes) == 0 && len(c.Tags) == 0 && len(c.Own) == 0
}

// autofillColumns are the device columns autofill may write. The map is also
// the guard that keeps a field name out of the SQL unless it is one of these.
var autofillColumns = map[string]string{
	"vendor": "vendor", "model": "model", "kind": "kind",
	"icon": "icon", "function": "function", "name": "name",
}

// ReplaceHints makes hints the complete set source has for the device.
func (s *Store) ReplaceHints(ctx context.Context, deviceID int64, source string, hints []Hint) error {
	return s.withTx(ctx, func(c conn) error {
		if _, err := s.execOn(ctx, c, `DELETE FROM device_hint WHERE device_id=? AND source=?`, deviceID, source); err != nil {
			return err
		}
		for _, h := range hints {
			if _, err := s.execOn(ctx, c, `INSERT INTO device_hint (device_id,source,field,value,confidence,detail,seen_at)
				VALUES (?,?,?,?,?,?,?)`, deviceID, source, h.Field, h.Value, h.Confidence, h.Detail, h.SeenAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListHints returns every hint for a device, by field, then confidence
// (highest first), then source and value.
func (s *Store) ListHints(ctx context.Context, deviceID int64) ([]Hint, error) {
	rows, err := s.query(ctx, `SELECT device_id,source,field,value,confidence,detail,seen_at
		FROM device_hint WHERE device_id=? ORDER BY field, confidence DESC, source, value`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hint
	for rows.Next() {
		var h Hint
		if err := rows.Scan(&h.DeviceID, &h.Source, &h.Field, &h.Value, &h.Confidence, &h.Detail, &h.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListAutofill returns the device's autofill records, by field.
func (s *Store) ListAutofill(ctx context.Context, deviceID int64) ([]AutofillRecord, error) {
	rows, err := s.query(ctx, `SELECT device_id,field,value,source,state,updated_at
		FROM device_autofill WHERE device_id=? ORDER BY field`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutofillRecord
	for rows.Next() {
		var r AutofillRecord
		if err := rows.Scan(&r.DeviceID, &r.Field, &r.Value, &r.Source, &r.State, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApplyAutofill applies ch in one transaction and returns the writes and
// tags that took effect. It never marks the device reviewed (compare
// UpdateDevice). A write whose Expect no longer matches is skipped silently:
// someone changed the field meanwhile, and the next pass will see that.
func (s *Store) ApplyAutofill(ctx context.Context, deviceID int64, ch AutofillChanges) ([]AutofillWrite, []AutofillTag, error) {
	for _, w := range ch.Writes {
		if _, ok := autofillColumns[w.Field]; !ok {
			return nil, nil, fmt.Errorf("autofill: unknown field %q", w.Field)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var writes []AutofillWrite
	var tags []AutofillTag
	err := s.withTx(ctx, func(c conn) error {
		writes, tags = nil, nil
		for _, f := range ch.Own {
			if _, err := s.execOn(ctx, c, `UPDATE device_autofill SET state=?, updated_at=? WHERE device_id=? AND field=?`,
				AutofillOwned, now, deviceID, f); err != nil {
				return err
			}
		}
		for _, w := range ch.Writes {
			col := autofillColumns[w.Field]
			q := `UPDATE device SET ` + col + `=? WHERE id=? AND ` + col + `=?`
			if w.Unreviewed {
				q += ` AND reviewed=FALSE`
			}
			res, err := s.execOn(ctx, c, q, w.Value, deviceID, w.Expect)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if err := s.recordAutofillOn(ctx, c, deviceID, w.Field, w.Value, w.Source, now); err != nil {
				return err
			}
			writes = append(writes, w)
		}
		for _, t := range ch.Tags {
			tagID, err := s.findOrCreateTagOn(ctx, c, t.Name)
			if err != nil {
				return err
			}
			res, err := s.execOn(ctx, c, `INSERT INTO device_tag (device_id,tag_id) VALUES (?,?) ON CONFLICT DO NOTHING`, deviceID, tagID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if err := s.recordAutofillOn(ctx, c, deviceID, "tag:"+t.Name, t.Name, t.Source, now); err != nil {
				return err
			}
			tags = append(tags, t)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return writes, tags, nil
}

func (s *Store) recordAutofillOn(ctx context.Context, c conn, deviceID int64, field, value, source, now string) error {
	_, err := s.execOn(ctx, c, `INSERT INTO device_autofill (device_id,field,value,source,state,updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(device_id,field) DO UPDATE SET value=excluded.value, source=excluded.source,
			state=excluded.state, updated_at=excluded.updated_at`,
		deviceID, field, value, source, AutofillApplied, now)
	return err
}

// DeviceIDs returns every device id, ascending.
func (s *Store) DeviceIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.query(ctx, `SELECT id FROM device ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run the store tests**

Run: `go test ./internal/store/...`
Expected: PASS, including `TestDeviceRebuildKeepsAddedColumns`. If Postgres is available (`make pg`), also run `make test-pg` scoped: `NETIS_TEST_PG_DSN=postgres://netis:netis@127.0.0.1:55432/netis?sslmode=disable go test ./internal/store/...` — PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat(store): device hints and autofill records"
```

---

### Task 3: Resolver, apply rules and explanations

**Files:**
- Create: `internal/autofill/resolve.go`
- Test: `internal/autofill/resolve_test.go`

**Interfaces:**
- Consumes: `store.Hint`, `store.AutofillRecord`, `store.AutofillChanges`, `store.AutofillWrite`, `store.AutofillTag`, `store.Device` (Task 2).
- Produces (package `autofill`):

```go
const Threshold = 50
type Candidate struct{ Value, Source, Detail string; Confidence int }
func Resolve(hints []store.Hint) (fields map[string]Candidate, tags []Candidate)
func decide(d store.Device, tags []string, recs []store.AutofillRecord, fields map[string]Candidate, tagCands []Candidate) store.AutofillChanges
func IsPlaceholderName(name string) bool
type Explained struct{ store.Hint; Status string }
const (StatusApplied = "applied"; StatusLow = "low"; StatusOwned = "owned"; StatusOutranked = "outranked"; StatusKept = "kept")
func Explain(d store.Device, tags []string, recs []store.AutofillRecord, hints []store.Hint) []Explained
```

- [ ] **Step 1: Write the failing tests**

`internal/autofill/resolve_test.go`:

```go
package autofill

import (
	"reflect"
	"testing"

	"netis/internal/store"
)

func h(source, field, value string, conf int) store.Hint {
	return store.Hint{Source: source, Field: field, Value: value, Confidence: conf}
}

func TestResolvePicksHighestThenRank(t *testing.T) {
	fields, tags := Resolve([]store.Hint{
		h("oui", "vendor", "Apple", 90),
		h("hostname", "vendor", "Samsung", 70),
		h("hostname", "kind", "phone", 70),
		h("ports", "kind", "printer", 70), // tie: ports outranks hostname
		h("oui", "kind", "iot", 40),       // below threshold
		h("hostname", "tag", "media", 60),
		h("ports", "tag", "media", 50),
		h("ports", "tag", "mqtt", 49), // below threshold
	})
	if fields["vendor"].Value != "Apple" || fields["kind"].Value != "printer" || fields["kind"].Source != "ports" {
		t.Fatalf("fields = %+v", fields)
	}
	if len(tags) != 1 || tags[0].Value != "media" || tags[0].Confidence != 60 {
		t.Fatalf("tags = %+v", tags)
	}
	f, _ := Resolve([]store.Hint{h("oui", "kind", "iot", 40)})
	if len(f) != 0 {
		t.Fatalf("low hint resolved: %+v", f)
	}
}

func TestPlaceholderName(t *testing.T) {
	for name, want := range map[string]bool{
		"unknown-bc:24:11:00:00:01": true,
		"unknown-10.0.0.9":          true,
		"private-da:a1:19:00:00:01": true,
		"pihole-aa:bb:cc:dd:ee:ff":  true,
		"adguard-aa:bb:cc:dd:ee:ff": true,
		"opnsense-aa:bb:cc:dd:ee:ff": true,
		"printer":                   false,
		"unknown-thing":             false,
	} {
		if got := IsPlaceholderName(name); got != want {
			t.Errorf("IsPlaceholderName(%q) = %v", name, got)
		}
	}
}

func cand(v, src string) Candidate { return Candidate{Value: v, Source: src, Confidence: 80} }

func TestDecideRules(t *testing.T) {
	applied := func(f, v string) store.AutofillRecord {
		return store.AutofillRecord{Field: f, Value: v, Source: "hostname", State: store.AutofillApplied}
	}
	owned := func(f, v string) store.AutofillRecord {
		return store.AutofillRecord{Field: f, Value: v, State: store.AutofillOwned}
	}
	cases := []struct {
		name   string
		dev    store.Device
		tags   []string
		recs   []store.AutofillRecord
		fields map[string]Candidate
		tagC   []Candidate
		want   store.AutofillChanges
	}{
		{
			name:   "empty field is filled",
			dev:    store.Device{Kind: "other"},
			fields: map[string]Candidate{"vendor": cand("Brother", "oui"), "kind": cand("printer", "hostname")},
			want: store.AutofillChanges{Writes: []store.AutofillWrite{
				{Field: "vendor", Value: "Brother", Source: "oui", Expect: ""},
				{Field: "kind", Value: "printer", Source: "hostname", Expect: "other"},
			}},
		},
		{
			name:   "owned field is skipped",
			dev:    store.Device{Kind: "other", Vendor: ""},
			recs:   []store.AutofillRecord{owned("vendor", "Brother")},
			fields: map[string]Candidate{"vendor": cand("Brother", "oui")},
		},
		{
			name:   "person changed an applied value: becomes owned",
			dev:    store.Device{Kind: "other", Vendor: "Mine"},
			recs:   []store.AutofillRecord{applied("vendor", "Brother")},
			fields: map[string]Candidate{"vendor": cand("Brother", "oui")},
			want:   store.AutofillChanges{Own: []string{"vendor"}},
		},
		{
			name: "person cleared an applied value: becomes owned",
			dev:  store.Device{Kind: "other"},
			recs: []store.AutofillRecord{applied("vendor", "Brother")},
			want: store.AutofillChanges{Own: []string{"vendor"}},
		},
		{
			name:   "better source replaces applied value while unreviewed",
			dev:    store.Device{Kind: "phone"},
			recs:   []store.AutofillRecord{applied("kind", "phone")},
			fields: map[string]Candidate{"kind": cand("computer", "mdns")},
			want: store.AutofillChanges{Writes: []store.AutofillWrite{
				{Field: "kind", Value: "computer", Source: "mdns", Expect: "phone", Unreviewed: true},
			}},
		},
		{
			name:   "reviewed device keeps applied value",
			dev:    store.Device{Kind: "phone", Reviewed: true},
			recs:   []store.AutofillRecord{applied("kind", "phone")},
			fields: map[string]Candidate{"kind": cand("computer", "mdns")},
		},
		{
			name:   "value set by someone else is kept",
			dev:    store.Device{Kind: "vm", Vendor: "Dell"},
			fields: map[string]Candidate{"vendor": cand("Intel", "oui"), "kind": cand("computer", "hostname")},
		},
		{
			name:   "placeholder name on unreviewed device is filled",
			dev:    store.Device{Kind: "other", Name: "unknown-10.0.0.9"},
			fields: map[string]Candidate{"name": cand("Living room TV", "mdns")},
			want: store.AutofillChanges{Writes: []store.AutofillWrite{
				{Field: "name", Value: "Living room TV", Source: "mdns", Expect: "unknown-10.0.0.9", Unreviewed: true},
			}},
		},
		{
			name:   "placeholder name on reviewed device is kept",
			dev:    store.Device{Kind: "other", Name: "unknown-10.0.0.9", Reviewed: true},
			fields: map[string]Candidate{"name": cand("Living room TV", "mdns")},
		},
		{
			name: "tags: add new, skip present, removed becomes owned, owned skipped",
			dev:  store.Device{Kind: "other"},
			tags: []string{"media", "mine"},
			recs: []store.AutofillRecord{applied("tag:nas", "nas"), owned("tag:camera", "camera")},
			tagC: []Candidate{cand("media", "hostname"), cand("smart-home", "ports"), cand("nas", "hostname"), cand("camera", "ports")},
			want: store.AutofillChanges{
				Tags: []store.AutofillTag{{Name: "smart-home", Source: "ports"}},
				Own:  []string{"tag:nas"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decide(c.dev, c.tags, c.recs, c.fields, c.tagC)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("decide =\n %+v\nwant\n %+v", got, c.want)
			}
		})
	}
}

func TestExplain(t *testing.T) {
	d := store.Device{Kind: "printer", Vendor: "Mine", Model: "HL"}
	recs := []store.AutofillRecord{
		{Field: "kind", Value: "printer", Source: "hostname", State: store.AutofillApplied},
		{Field: "vendor", Value: "Brother", State: store.AutofillOwned},
	}
	hints := []store.Hint{
		h("hostname", "kind", "printer", 80),
		h("oui", "kind", "iot", 40),
		h("ports", "kind", "server", 60),
		h("oui", "vendor", "Brother", 90),
		h("hostname", "model", "HL-L2350", 60),
	}
	got := map[string]string{}
	for _, e := range Explain(d, nil, recs, hints) {
		got[e.Source+"/"+e.Field] = e.Status
	}
	want := map[string]string{
		"hostname/kind": StatusApplied,
		"oui/kind":      StatusLow,
		"ports/kind":    StatusOutranked,
		"oui/vendor":    StatusOwned,
		"hostname/model": StatusKept,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Explain = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/autofill/`
Expected: FAIL, undefined `Resolve`.

- [ ] **Step 3: Implement `internal/autofill/resolve.go`**

```go
// Package autofill fills in device details (vendor, model, kind, icon,
// function, name, tags) from hints that sources observe, without ever
// overwriting what a person set. See
// docs/superpowers/specs/2026-09-29-netis-autofill-design.md.
package autofill

import (
	"regexp"
	"sort"
	"strings"

	"netis/internal/store"
)

// Threshold is the lowest confidence that is applied. Lower hints are kept
// and shown as suggestions only.
const Threshold = 50

// Field names, matching device columns (and "tag").
const (
	FieldVendor   = "vendor"
	FieldModel    = "model"
	FieldKind     = "kind"
	FieldIcon     = "icon"
	FieldFunction = "function"
	FieldName     = "name"
	FieldTag      = "tag"
)

// singleFields are the one-value fields, in the order changes are listed.
var singleFields = []string{FieldVendor, FieldModel, FieldKind, FieldIcon, FieldFunction, FieldName}

// sourceRank breaks confidence ties: a source that looked closer wins.
var sourceRank = map[string]int{"mdns": 5, "ssdp": 4, "ports": 3, "hostname": 2, "oui": 1}

// Candidate is the value a field would get, and why.
type Candidate struct {
	Value, Source, Detail string
	Confidence            int
}

func better(a store.Hint, b Candidate) bool {
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	if ra, rb := sourceRank[a.Source], sourceRank[b.Source]; ra != rb {
		return ra > rb
	}
	return a.Value < b.Value
}

// Resolve picks the winning candidate per single-valued field and the tags
// to add, ignoring hints below Threshold. Tags are sorted by name.
func Resolve(hints []store.Hint) (map[string]Candidate, []Candidate) {
	fields := map[string]Candidate{}
	tagBy := map[string]Candidate{}
	for _, h := range hints {
		if h.Confidence < Threshold || h.Value == "" {
			continue
		}
		c := Candidate{Value: h.Value, Source: h.Source, Detail: h.Detail, Confidence: h.Confidence}
		if h.Field == FieldTag {
			if cur, ok := tagBy[h.Value]; !ok || better(h, cur) {
				tagBy[h.Value] = c
			}
			continue
		}
		if cur, ok := fields[h.Field]; !ok || better(h, cur) {
			fields[h.Field] = c
		}
	}
	tags := make([]Candidate, 0, len(tagBy))
	for _, c := range tagBy {
		tags = append(tags, c)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Value < tags[j].Value })
	return fields, tags
}

// placeholderRe matches the names netis makes up for a device it knows
// nothing about: unknown-<mac|ip> and private-<mac> from a sweep,
// <source>-<mac> from a lease sync.
var placeholderRe = regexp.MustCompile(`^(unknown|private|pihole|adguard|opnsense)-[0-9a-fA-F:.]+$`)

// IsPlaceholderName reports whether name is one netis made up.
func IsPlaceholderName(name string) bool { return placeholderRe.MatchString(name) }

func current(d store.Device, field string) string {
	switch field {
	case FieldVendor:
		return d.Vendor
	case FieldModel:
		return d.Model
	case FieldKind:
		return d.Kind
	case FieldIcon:
		return d.Icon
	case FieldFunction:
		return d.Function
	case FieldName:
		return d.Name
	}
	return ""
}

// isEmpty reports whether a field holds nothing a person would miss.
func isEmpty(d store.Device, field, cur string) bool {
	switch field {
	case FieldKind:
		return cur == "" || cur == "other"
	case FieldName:
		return !d.Reviewed && IsPlaceholderName(cur)
	}
	return cur == ""
}

func recordsByField(recs []store.AutofillRecord) map[string]store.AutofillRecord {
	m := make(map[string]store.AutofillRecord, len(recs))
	for _, r := range recs {
		m[r.Field] = r
	}
	return m
}

// decide applies the ownership rules to one device. It is pure: the store
// re-checks each write against the value read here (AutofillWrite.Expect).
func decide(d store.Device, tags []string, recs []store.AutofillRecord, fields map[string]Candidate, tagCands []Candidate) store.AutofillChanges {
	by := recordsByField(recs)
	var ch store.AutofillChanges
	for _, f := range singleFields {
		cur := current(d, f)
		rec, has := by[f]
		if has && rec.State == store.AutofillOwned {
			continue
		}
		if has && cur != rec.Value {
			ch.Own = append(ch.Own, f)
			continue
		}
		c, ok := fields[f]
		if !ok {
			continue
		}
		switch {
		case isEmpty(d, f, cur):
			ch.Writes = append(ch.Writes, store.AutofillWrite{Field: f, Value: c.Value, Source: c.Source,
				Expect: cur, Unreviewed: f == FieldName})
		case has && !d.Reviewed && c.Value != cur:
			ch.Writes = append(ch.Writes, store.AutofillWrite{Field: f, Value: c.Value, Source: c.Source,
				Expect: cur, Unreviewed: true})
		}
	}

	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[t] = true
	}
	for _, r := range recs {
		if name, ok := strings.CutPrefix(r.Field, "tag:"); ok && r.State == store.AutofillApplied && !have[name] {
			ch.Own = append(ch.Own, r.Field)
		}
	}
	for _, c := range tagCands {
		if _, has := by["tag:"+c.Value]; has || have[c.Value] {
			continue
		}
		ch.Tags = append(ch.Tags, store.AutofillTag{Name: c.Value, Source: c.Source})
	}
	return ch
}

// Hint statuses, for the device page.
const (
	StatusApplied   = "applied"   // the device shows this value because of this hint
	StatusLow       = "low"       // below Threshold: a suggestion only
	StatusOwned     = "owned"     // a person set this field; autofill keeps out
	StatusOutranked = "outranked" // another hint for the field won
	StatusKept      = "kept"      // the field already had a value autofill did not write
)

// Explained is a hint with what became of it.
type Explained struct {
	store.Hint
	Status string
}

// Explain says, for every hint, why the device does or does not show it.
func Explain(d store.Device, tags []string, recs []store.AutofillRecord, hints []store.Hint) []Explained {
	by := recordsByField(recs)
	fields, _ := Resolve(hints)
	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[t] = true
	}
	out := make([]Explained, 0, len(hints))
	for _, h := range hints {
		key := h.Field
		cur := current(d, h.Field)
		if h.Field == FieldTag {
			key = "tag:" + h.Value
		}
		rec, has := by[key]
		var st string
		switch {
		case has && rec.State == store.AutofillOwned:
			st = StatusOwned
		case h.Confidence < Threshold:
			st = StatusLow
		case h.Field == FieldTag && has && have[h.Value]:
			st = StatusApplied
		case h.Field == FieldTag:
			st = StatusKept
		case has && cur == rec.Value && cur == h.Value:
			st = StatusApplied
		case fields[h.Field].Source != h.Source || fields[h.Field].Value != h.Value:
			st = StatusOutranked
		default:
			st = StatusKept
		}
		out = append(out, Explained{Hint: h, Status: st})
	}
	return out
}
```

Note on `Explain` for tags without a record: a tag present on the device that autofill did not add is `kept`; a tag not yet added (pass pending) is also `kept` — acceptable, the next pass adds it.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/autofill/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autofill
git commit -m "feat(autofill): resolve hints and decide what to fill"
```

---

### Task 4: Local sources — OUI, hostname and port rules

**Files:**
- Create: `internal/autofill/sources.go`
- Test: `internal/autofill/sources_test.go`

**Interfaces:**
- Consumes: `oui.Vendor` (Task 1), `store.Hint` (Task 2), field constants (Task 3).
- Produces: `ouiHints(macs []string) []store.Hint`, `hostnameHints(names []named) []store.Hint` with `type named struct{ Label, Value string }` (Label is `hostname` or `name`), `portHints(open []int) []store.Hint`, `dedupe([]store.Hint) []store.Hint`. Hints carry Field, Value, Confidence, Detail only; the service stamps DeviceID, Source, SeenAt.

- [ ] **Step 1: Write the failing tests**

`internal/autofill/sources_test.go`:

```go
package autofill

import (
	"testing"

	"netis/internal/store"
)

// has reports whether hints contain field=value at confidence conf.
func has(hints []store.Hint, field, value string, conf int) bool {
	for _, h := range hints {
		if h.Field == field && h.Value == value && h.Confidence == conf {
			return true
		}
	}
	return false
}

func TestOUIHints(t *testing.T) {
	hs := ouiHints([]string{"3c:07:54:00:00:01", "da:a1:19:00:00:01", ""})
	if len(hs) != 1 || !has(hs, "vendor", "Apple", 90) || hs[0].Detail != "MAC 3c:07:54:00:00:01" {
		t.Fatalf("hints = %+v", hs)
	}
	// Espressif makes one thing: kind iot as a suggestion (40).
	hs = ouiHints([]string{"24:0a:c4:00:00:01"})
	if !has(hs, "vendor", "Espressif", 90) || !has(hs, "kind", "iot", 40) {
		t.Fatalf("hints = %+v", hs)
	}
}

func TestHostnameRules(t *testing.T) {
	type want struct {
		field, value string
		conf         int
	}
	cases := map[string][]want{
		"Bens-iPhone":               {{"kind", "phone", 70}, {"vendor", "Apple", 70}, {"model", "iPhone", 60}},
		"iPad.lan":                  {{"kind", "phone", 60}, {"icon", "tablet", 70}, {"model", "iPad", 60}},
		"MacBook-Pro-3.local":       {{"kind", "computer", 70}, {"model", "MacBook Pro", 65}},
		"MacBook-Air":               {{"model", "MacBook Air", 65}},
		"imac":                      {{"model", "iMac", 60}},
		"Mac-mini":                  {{"model", "Mac mini", 60}},
		"Galaxy-S23":                {{"kind", "phone", 70}, {"vendor", "Samsung", 70}},
		"SM-G991B":                  {{"vendor", "Samsung", 70}},
		"Pixel-8":                   {{"vendor", "Google", 70}},
		"android-3f2a9c":            {{"kind", "phone", 60}},
		"DESKTOP-AB12CD3":           {{"kind", "computer", 70}},
		"LAPTOP-9K2M4X1":            {{"kind", "computer", 70}},
		"BRW3C2AF4A1B2C3":           {{"kind", "printer", 80}, {"vendor", "Brother", 80}},
		"HP3C2AF4":                  {{"kind", "printer", 80}, {"vendor", "HP", 80}},
		"NPI3C2AF4":                 {{"vendor", "HP", 80}},
		"EPSON1A2B3C":               {{"kind", "printer", 70}, {"vendor", "Epson", 70}},
		"ESP-1A2B3C":                {{"kind", "iot", 70}, {"tag", "smart-home", 60}},
		"shelly1pm-ABC":             {{"kind", "iot", 70}},
		"tasmota-1234":              {{"kind", "iot", 70}},
		"Chromecast":                {{"icon", "tv", 70}, {"tag", "media", 60}},
		"Apple-TV":                  {{"icon", "tv", 70}},
		"Sonos-Kitchen":             {{"icon", "speaker", 70}, {"vendor", "Sonos", 70}},
		"PS5-123":                   {{"icon", "gamepad-2", 70}},
		"XBOX":                      {{"icon", "gamepad-2", 70}},
		"raspberrypi":               {{"kind", "computer", 60}, {"vendor", "Raspberry Pi", 60}},
		"diskstation":               {{"kind", "server", 60}, {"tag", "nas", 60}},
		"nas01":                     {{"tag", "nas", 60}},
		"pve":                       {{"kind", "server", 70}, {"function", "Proxmox VE", 60}},
		"pve2.home.arpa":            {{"function", "Proxmox VE", 60}},
	}
	for hn, wants := range cases {
		hs := hostnameHints([]named{{Label: "hostname", Value: hn}})
		for _, w := range wants {
			if !has(hs, w.field, w.value, w.conf) {
				t.Errorf("%s: missing %s=%s@%d in %+v", hn, w.field, w.value, w.conf, hs)
			}
		}
	}
	for _, hn := range []string{"dynasty", "unknown-bc:24:11:00:00:01", "private-da:a1:19:00:00:01", "pihole-aa:bb:cc:dd:ee:ff", "printer-room", "espresso"} {
		if hs := hostnameHints([]named{{Label: "hostname", Value: hn}}); len(hs) != 0 {
			t.Errorf("%s: unexpected hints %+v", hn, hs)
		}
	}
	hs := hostnameHints([]named{{Label: "hostname", Value: "BRW3C2AF4A1B2C3"}})
	if hs[0].Detail != `hostname BRW3C2AF4A1B2C3` {
		t.Errorf("detail = %q", hs[0].Detail)
	}
}

func TestPortHints(t *testing.T) {
	hs := portHints([]int{22, 9100, 8006, 53})
	if !has(hs, "kind", "printer", 70) || !has(hs, "kind", "server", 80) ||
		!has(hs, "function", "Proxmox VE", 80) || !has(hs, "function", "DNS", 50) {
		t.Fatalf("hints = %+v", hs)
	}
	for _, h := range hs {
		if h.Field == "kind" && h.Value == "printer" && h.Detail != "port 9100 open" {
			t.Errorf("detail = %q", h.Detail)
		}
	}
	if hs := portHints([]int{22, 443}); len(hs) != 0 {
		t.Errorf("ssh/https alone say nothing: %+v", hs)
	}
}

func TestDedupeKeepsHighest(t *testing.T) {
	got := dedupe([]store.Hint{
		{Field: "kind", Value: "server", Confidence: 60, Detail: "a"},
		{Field: "kind", Value: "server", Confidence: 70, Detail: "b"},
		{Field: "tag", Value: "nas", Confidence: 60},
	})
	if len(got) != 2 || !has(got, "kind", "server", 70) {
		t.Fatalf("dedupe = %+v", got)
	}
}
```

If the real-registry Espressif name differs (check `zcat internal/oui/oui.txt.gz | grep -i espressif`), keep the test's `"Espressif"` and add an override in `internal/oui/normalize.go`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/autofill/ -run 'OUI|Hostname|Port|Dedupe'`
Expected: FAIL, undefined `ouiHints`.

- [ ] **Step 3: Implement `internal/autofill/sources.go`**

```go
package autofill

import (
	"fmt"
	"regexp"
	"strings"

	"netis/internal/oui"
	"netis/internal/store"
)

func hint(field, value string, conf int) store.Hint {
	return store.Hint{Field: field, Value: value, Confidence: conf}
}

// vendorKinds are makers that make essentially one kind of device. The hint
// is below Threshold: a suggestion, not a fill.
var vendorKinds = map[string]string{
	"Espressif":  "iot",
	"Tuya Smart": "iot",
	"Shelly":     "iot",
	"Brother":    "printer",
	"Epson":      "printer",
}

// ouiHints names each MAC's maker.
func ouiHints(macs []string) []store.Hint {
	var out []store.Hint
	for _, mac := range macs {
		v := oui.Vendor(mac)
		if v == "" {
			continue
		}
		detail := "MAC " + mac
		h := hint(FieldVendor, v, 90)
		h.Detail = detail
		out = append(out, h)
		if k, ok := vendorKinds[v]; ok {
			h := hint(FieldKind, k, 40)
			h.Detail = detail
			out = append(out, h)
		}
	}
	return dedupe(out)
}

// named is a name the hostname rules look at, and what it is ("hostname" or
// "name"), for the evidence shown to people.
type named struct{ Label, Value string }

type hostRule struct {
	re    *regexp.Regexp
	hints []store.Hint
}

func rule(pattern string, hints ...store.Hint) hostRule {
	return hostRule{re: regexp.MustCompile(pattern), hints: hints}
}

// sep matches the start or end of a word inside a hostname label.
const (
	pre  = `(^|[-_.])`
	post = `([-_.0-9]|$)`
)

// hostRules match the first label of a hostname, lower-cased. Each rule's
// patterns are exercised in sources_test.go.
var hostRules = []hostRule{
	rule(`iphone`, hint(FieldKind, "phone", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "iPhone", 60)),
	rule(`ipad`, hint(FieldKind, "phone", 60), hint(FieldIcon, "tablet", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "iPad", 60)),
	rule(`macbook-?pro`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "MacBook Pro", 65)),
	rule(`macbook-?air`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "MacBook Air", 65)),
	rule(`macbook`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "MacBook", 60)),
	rule(`imac`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "iMac", 60)),
	rule(`mac-?mini`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "Mac mini", 60)),
	rule(`galaxy|^sm-[a-z]\d`, hint(FieldKind, "phone", 70), hint(FieldVendor, "Samsung", 70)),
	rule(`^pixel`, hint(FieldKind, "phone", 70), hint(FieldVendor, "Google", 70)),
	rule(`^android-`, hint(FieldKind, "phone", 60)),
	rule(`^(desktop|laptop)-[a-z0-9]{7}$`, hint(FieldKind, "computer", 70)),
	rule(`^br[wn][0-9a-f]{12}$`, hint(FieldKind, "printer", 80), hint(FieldVendor, "Brother", 80)),
	rule(`^(hp|npi)[0-9a-f]{6}`, hint(FieldKind, "printer", 80), hint(FieldVendor, "HP", 80)),
	rule(`epson`, hint(FieldKind, "printer", 70), hint(FieldVendor, "Epson", 70)),
	rule(`^esp[-_]|tasmota|shelly|esphome`, hint(FieldKind, "iot", 70), hint(FieldTag, "smart-home", 60)),
	rule(`chromecast|roku|apple-?tv|fire-?tv`, hint(FieldIcon, "tv", 70), hint(FieldTag, "media", 60)),
	rule(`sonos`, hint(FieldIcon, "speaker", 70), hint(FieldVendor, "Sonos", 70), hint(FieldTag, "media", 60)),
	rule(pre+`ps[45]`+post+`|xbox|nintendo`, hint(FieldIcon, "gamepad-2", 70)),
	rule(`raspberrypi`, hint(FieldKind, "computer", 60), hint(FieldVendor, "Raspberry Pi", 60)),
	rule(`synology|diskstation|truenas|`+pre+`nas`+post, hint(FieldKind, "server", 60), hint(FieldTag, "nas", 60)),
	rule(`^pve`+post+`|proxmox`, hint(FieldKind, "server", 70), hint(FieldFunction, "Proxmox VE", 60)),
}

// hostnameHints runs the rules over each name's first label. Placeholder
// names netis made up are skipped.
func hostnameHints(names []named) []store.Hint {
	var out []store.Hint
	for _, n := range names {
		if n.Value == "" || IsPlaceholderName(n.Value) {
			continue
		}
		label, _, _ := strings.Cut(strings.ToLower(n.Value), ".")
		for _, r := range hostRules {
			if !r.re.MatchString(label) {
				continue
			}
			for _, h := range r.hints {
				h.Detail = n.Label + " " + n.Value
				out = append(out, h)
			}
		}
	}
	return dedupe(out)
}

type portRule struct {
	ports []int
	hints []store.Hint
}

// portRules turn an open port into hints; ports alone that every host has
// (22, 80, 443) say nothing.
var portRules = []portRule{
	{[]int{9100, 631}, []store.Hint{hint(FieldKind, "printer", 70)}},
	{[]int{554}, []store.Hint{hint(FieldIcon, "cctv", 60), hint(FieldTag, "camera", 60)}},
	{[]int{8006}, []store.Hint{hint(FieldKind, "server", 80), hint(FieldFunction, "Proxmox VE", 80)}},
	{[]int{8123}, []store.Hint{hint(FieldFunction, "Home Assistant", 70), hint(FieldTag, "smart-home", 60)}},
	{[]int{32400}, []store.Hint{hint(FieldFunction, "Plex", 70), hint(FieldTag, "media", 60)}},
	{[]int{3389}, []store.Hint{hint(FieldKind, "computer", 60)}},
	{[]int{53}, []store.Hint{hint(FieldFunction, "DNS", 50)}},
	{[]int{1883}, []store.Hint{hint(FieldTag, "mqtt", 50)}},
}

// portHints turns recorded open ports into hints.
func portHints(open []int) []store.Hint {
	isOpen := make(map[int]bool, len(open))
	for _, p := range open {
		isOpen[p] = true
	}
	var out []store.Hint
	for _, r := range portRules {
		for _, p := range r.ports {
			if !isOpen[p] {
				continue
			}
			for _, h := range r.hints {
				h.Detail = fmt.Sprintf("port %d open", p)
				out = append(out, h)
			}
			break
		}
	}
	return dedupe(out)
}

// dedupe keeps one hint per field and value, the most confident, so a
// source's set fits device_hint's primary key.
func dedupe(hints []store.Hint) []store.Hint {
	idx := map[[2]string]int{}
	var out []store.Hint
	for _, h := range hints {
		k := [2]string{h.Field, h.Value}
		if i, ok := idx[k]; ok {
			if h.Confidence > out[i].Confidence {
				out[i] = h
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, h)
	}
	return out
}
```

Watch-outs when running the tests: `printer-room` must not match (no rule has bare `printer`); `espresso` must not match `^esp[-_]`; `dynasty` must not match the `nas` rule (it needs a word boundary on both sides); `unknown-…` is skipped as a placeholder. `^pve` + `post` accepts `pve`, `pve2`, `pve-node`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/autofill/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/autofill internal/oui
git commit -m "feat(autofill): hints from MAC vendor, hostname and open ports"
```

---

### Task 5: Autofill service

**Files:**
- Create: `internal/autofill/service.go`
- Test: `internal/autofill/service_test.go`
- Modify: `internal/scan/engine.go` (drop vendor at create), `internal/scan/engine_test.go`

**Interfaces:**
- Consumes: Tasks 2–4.
- Produces:

```go
type Service struct{ /* unexported */ }
func New(st *store.Store) *Service
func (s *Service) Run(ctx context.Context, ids ...int64) error // no ids = every device
func (s *Service) Kick()
func (s *Service) Start(ctx context.Context) // blocks until ctx done
func Enabled(ctx context.Context, st *store.Store) bool // setting autofill_enabled != "off"
```

- [ ] **Step 1: Write the failing test**

`internal/autofill/service_test.go`:

```go
package autofill

import (
	"context"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

// discovered creates a device the way a sweep does, in 10.0.0.0/24.
func discovered(t *testing.T, st *store.Store, name, mac, hostname, ip string) int64 {
	t.Helper()
	subs, err := st.ListSubnets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var sn int64
	if len(subs) > 0 {
		sn = subs[0].ID
	} else if sn, err = st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan"}); err != nil {
		t.Fatal(err)
	}
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	id, _, err := st.CreateDiscoveredDevice(t.Context(), store.Device{Name: name, Kind: "other", Source: "scan"},
		&mac, hp, sn, ip, "dhcp")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestServiceFillsAndRespectsPeople(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		svc := New(st)
		id := discovered(t, st, "BRW3C2AF4A1B2C3", "00:1b:a9:00:00:01", "BRW3C2AF4A1B2C3", "10.0.0.5")
		if err := svc.Run(ctx); err != nil {
			t.Fatal(err)
		}
		d, _ := st.GetDevice(ctx, id)
		if d.Kind != "printer" || d.Vendor != "Brother" || d.Reviewed {
			t.Fatalf("after first pass: %+v", d)
		}
		// Audit entry names netis and what changed.
		entries, _, _ := st.ListAudit(ctx, store.AuditFilter{Action: "device.autofill", Limit: 10})
		if len(entries) != 1 || entries[0].Username != "netis" || !strings.Contains(entries[0].Detail, "kind=printer") {
			t.Fatalf("audit = %+v", entries)
		}

		// A person changes the vendor; the next pass keeps it and the field
		// becomes theirs.
		d.Vendor = "Mine"
		if err := st.UpdateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
		if err := svc.Run(ctx, id); err != nil {
			t.Fatal(err)
		}
		d, _ = st.GetDevice(ctx, id)
		if d.Vendor != "Mine" {
			t.Fatalf("vendor clobbered: %+v", d)
		}
		recs, _ := st.ListAutofill(ctx, id)
		for _, r := range recs {
			if r.Field == "vendor" && r.State != store.AutofillOwned {
				t.Fatalf("vendor record = %+v", r)
			}
		}
	})
}

func TestServiceRemovedTagStaysRemoved(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		svc := New(st)
		id := discovered(t, st, "diskstation", "00:11:32:00:00:02", "diskstation", "10.0.0.6")
		svc.Run(ctx)
		tags, _ := st.DeviceTags(ctx, id)
		if len(tags) != 1 || tags[0].Name != "nas" {
			t.Fatalf("tags = %+v", tags)
		}
		if err := st.SetDeviceTags(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		svc.Run(ctx)
		svc.Run(ctx)
		if tags, _ := st.DeviceTags(ctx, id); len(tags) != 0 {
			t.Fatalf("removed tag came back: %+v", tags)
		}
	})
}

func TestServiceOffDoesNothing(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		st.SetSetting(ctx, "autofill_enabled", "off")
		id := discovered(t, st, "BRW3C2AF4A1B2C3", "00:1b:a9:00:00:01", "BRW3C2AF4A1B2C3", "10.0.0.5")
		New(st).Run(ctx)
		if d, _ := st.GetDevice(ctx, id); d.Kind != "other" {
			t.Fatalf("filled while off: %+v", d)
		}
	})
}

func TestKickCoalescesAndStartStops(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		svc := New(st)
		svc.interval = 10 * time.Millisecond
		passes := 0
		svc.afterPass = func() { passes++ }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		for i := 0; i < 5; i++ {
			svc.Kick()
		}
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
		// startup pass + at most two for the burst of kicks
		if passes < 2 || passes > 3 {
			t.Fatalf("passes = %d", passes)
		}
	})
}
```

The Brother MAC prefix `00:1b:a9` must be registered to Brother in the generated data; check with `zcat internal/oui/oui.txt.gz | grep ^001BA9` and pick any Brother prefix if not. Same for `00:11:32` (Synology).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/autofill/ -run Service`
Expected: FAIL, undefined `New`.

- [ ] **Step 3: Implement `internal/autofill/service.go`**

```go
package autofill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"netis/internal/store"
)

// Service runs autofill passes: it recomputes the local hints (oui,
// hostname, ports) for devices, resolves every hint, and applies the result.
type Service struct {
	st       *store.Store
	kick     chan struct{}
	interval time.Duration // least time between kicked passes
	// afterPass is a test hook, called after each pass Start runs.
	afterPass func()
}

// New returns a service over st. Call Start to serve Kick.
func New(st *store.Store) *Service {
	return &Service{st: st, kick: make(chan struct{}, 1), interval: 10 * time.Second}
}

// Enabled reports whether the admin left autofill on (the default).
func Enabled(ctx context.Context, st *store.Store) bool {
	v, err := st.GetSetting(ctx, "autofill_enabled")
	if err != nil {
		slog.Error("reading autofill_enabled failed", "err", err)
		return false
	}
	return v != "off"
}

// Kick asks for a full pass without waiting. Kicks during a pass coalesce
// into one more pass.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Start runs a full pass now, then one per burst of kicks, at most once per
// interval, until ctx is done.
func (s *Service) Start(ctx context.Context) {
	for {
		if err := s.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("autofill pass failed", "err", err)
		}
		if s.afterPass != nil {
			s.afterPass()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.interval):
		}
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		}
	}
}

// Run fills in the given devices, or every device when none are given. An
// error on one device is logged and the rest still run; the first error is
// returned.
func (s *Service) Run(ctx context.Context, ids ...int64) error {
	if !Enabled(ctx, s.st) {
		return nil
	}
	if len(ids) == 0 {
		all, err := s.st.DeviceIDs(ctx)
		if err != nil {
			return err
		}
		ids = all
	}
	var first error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.runOne(ctx, id); err != nil {
			slog.Error("autofill device failed", "device_id", id, "err", err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Service) runOne(ctx context.Context, id int64) error {
	d, err := s.st.GetDevice(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ifaces, err := s.st.ListIfaces(ctx, id)
	if err != nil {
		return err
	}
	var macs []string
	names := []named{{Label: "name", Value: d.Name}}
	var ports []int
	for _, f := range ifaces {
		if f.MAC != nil && *f.MAC != "" {
			macs = append(macs, *f.MAC)
		}
		if f.Hostname != nil && *f.Hostname != "" {
			names = append(names, named{Label: "hostname", Value: *f.Hostname})
		}
		ops, err := s.st.ListOpenPorts(ctx, f.ID)
		if err != nil {
			return err
		}
		for _, p := range ops {
			ports = append(ports, p.Port)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, src := range []struct {
		name  string
		hints []store.Hint
	}{
		{"oui", ouiHints(macs)},
		{"hostname", hostnameHints(names)},
		{"ports", portHints(ports)},
	} {
		for i := range src.hints {
			src.hints[i].DeviceID, src.hints[i].Source, src.hints[i].SeenAt = id, src.name, now
		}
		if err := s.st.ReplaceHints(ctx, id, src.name, src.hints); err != nil {
			return err
		}
	}

	hints, err := s.st.ListHints(ctx, id)
	if err != nil {
		return err
	}
	tagRows, err := s.st.DeviceTags(ctx, id)
	if err != nil {
		return err
	}
	tags := make([]string, len(tagRows))
	for i, t := range tagRows {
		tags[i] = t.Name
	}
	recs, err := s.st.ListAutofill(ctx, id)
	if err != nil {
		return err
	}
	fields, tagCands := Resolve(hints)
	ch := decide(d, tags, recs, fields, tagCands)
	if ch.Empty() {
		return nil
	}
	writes, added, err := s.st.ApplyAutofill(ctx, id, ch)
	if err != nil {
		return err
	}
	if len(writes) == 0 && len(added) == 0 {
		return nil
	}
	return s.st.AddAudit(ctx, store.AuditEntry{
		Username: "netis", Action: "device.autofill",
		Target: fmt.Sprintf("device %d", id), Detail: auditDetail(writes, added), Status: 200,
	})
}

// auditDetail says what a pass changed and from where:
// "kind=printer, vendor=Brother, +tag nas (hostname, oui)".
func auditDetail(writes []store.AutofillWrite, tags []store.AutofillTag) string {
	var parts []string
	srcs := map[string]bool{}
	for _, w := range writes {
		parts = append(parts, w.Field+"="+w.Value)
		srcs[w.Source] = true
	}
	for _, t := range tags {
		parts = append(parts, "+tag "+t.Name)
		srcs[t.Source] = true
	}
	var ss []string
	for s := range srcs {
		ss = append(ss, s)
	}
	sort.Strings(ss)
	return strings.Join(parts, ", ") + " (" + strings.Join(ss, ", ") + ")"
}
```

- [ ] **Step 4: Remove vendor from the scan engine**

In `internal/scan/engine.go` `createUnknown`, change to `d := store.Device{Name: name, Kind: "other", Source: "scan"}` and drop the `oui` import. In `internal/scan/engine_test.go` `TestAutoCreatesUnknownDevice`, remove the `d.Vendor != …` condition (vendor now arrives via autofill, which the engine only kicks).

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/autofill/ ./internal/scan/ ./internal/store/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/autofill internal/scan
git commit -m "feat(autofill): service that fills devices and records it in the audit log"
```

---

### Task 6: Wire autofill into sweeps, integrations, the web server and settings

**Files:**
- Modify: `internal/scan/engine.go` (field + kick), `internal/scan/engine_test.go`
- Modify: `cmd/netis/main.go` (build, start, kick after runs)
- Modify: `internal/web/server.go` (`Options.Autofill`, field), `internal/web/devices.go` (port scan, create), `internal/web/apiwrite.go` (create), `internal/web/settings.go` (toggle)
- Modify: `internal/web/views/settings_network.templ` (toggle)
- Test: `internal/web/autofill_test.go`, `internal/scan/engine_test.go`

**Interfaces:**
- Consumes: `autofill.Service` (Task 5).
- Produces: `scan.Engine.Autofill Kicker` with `type Kicker interface{ Kick() }`; `web.Autofiller interface{ Run(ctx context.Context, ids ...int64) error; Kick() }`; `web.Options.Autofill Autofiller`; setting key `autofill_enabled` (`on`/`off`, missing = on).

- [ ] **Step 1: Write the failing engine test**

Append to `internal/scan/engine_test.go`:

```go
type countKicker struct{ n int }

func (k *countKicker) Kick() { k.n++ }

func TestSweepKicksAutofill(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	k := &countKicker{}
	e.Autofill = k
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1}}
	sn, _ := st.GetSubnet(t.Context(), snID)
	if err := e.RunSubnet(t.Context(), sn); err != nil {
		t.Fatal(err)
	}
	if k.n != 1 {
		t.Fatalf("kicks = %d", k.n)
	}
}
```

- [ ] **Step 2: Run to verify it fails, then implement**

Run: `go test ./internal/scan/ -run Kicks` — FAIL (`e.Autofill undefined`).

In `internal/scan/engine.go` add to `Engine`:

```go
	// Autofill is kicked after each sweep so new and changed devices get
	// their details filled in. Nil turns that off.
	Autofill Kicker
```

and the type near the top:

```go
// Kicker asks for work without waiting for it (autofill.Service).
type Kicker interface{ Kick() }
```

At the end of `RunSubnet`, before `return nil`:

```go
	if e.Autofill != nil {
		e.Autofill.Kick()
	}
```

Run: `go test ./internal/scan/` — PASS.

- [ ] **Step 3: Wire main**

In `cmd/netis/main.go`:
- import `netis/internal/autofill`;
- after `evs.SetSubscriber(notifier)` and before the engine: `af := autofill.New(st)`;
- in the engine literal: `Autofill: af,`;
- after `var bg sync.WaitGroup`: `bg.Go(func() { af.Start(ctx) })`;
- give `integrationRunner` an `after func()` field, called in `Run` right after `r.recordStatus(...)` when `err == nil` (`if err == nil && r.after != nil { r.after() }`), and set `runNow.after = af.Kick` after `newIntegrationRunner`;
- pass `Autofill: af,` in `web.Options`.

Check `cmd/netis/*_test.go` for `newRunner` tests; add one that `after` runs once on success and not on error:

```go
func TestRunnerCallsAfterOnSuccess(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	evs := events.NewService(st, events.NewBroker())
	calls := 0
	fail := false
	r := newRunner(st, evs, time.Second, map[string]integrationFunc{
		"x": func(context.Context) (int, string, error) {
			if fail {
				return 0, "", errors.New("boom")
			}
			return 1, "", nil
		},
	})
	r.after = func() { calls++ }
	r.Run(t.Context(), "x")
	fail = true
	r.Run(t.Context(), "x")
	if calls != 1 {
		t.Fatalf("after calls = %d", calls)
	}
}
```

(Adjust imports to match the existing test file.)

- [ ] **Step 4: Write the failing web tests**

`internal/web/autofill_test.go`:

```go
package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

type fakeAutofill struct {
	runs  [][]int64
	kicks int
}

func (f *fakeAutofill) Run(_ context.Context, ids ...int64) error { f.runs = append(f.runs, ids); return nil }
func (f *fakeAutofill) Kick()                                     { f.kicks++ }

func TestAutofillSettingSavesAndKicks(t *testing.T) {
	srv, st := testServer(t)
	af := &fakeAutofill{}
	srv.autofill = af
	addAdmin(t, st)
	c := loginClient(t, srv) // use the helper other settings tests use to post as the admin
	form := url.Values{"section": {"network"}, "offline_after": {"3"}, "autofill_enabled": {"off"}}
	rec := c.post("/settings/general", form)
	if rec.Code >= 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if v, _ := st.GetSetting(t.Context(), "autofill_enabled"); v != "off" {
		t.Fatalf("setting = %q", v)
	}
	if af.kicks != 1 {
		t.Fatalf("kicks = %d", af.kicks)
	}
	page := c.get("/settings/network")
	if !strings.Contains(page.Body.String(), `name="autofill_enabled"`) {
		t.Fatal("toggle missing from the Network page")
	}
	bad := c.post("/settings/general", url.Values{"section": {"network"}, "offline_after": {"3"}, "autofill_enabled": {"maybe"}})
	if bad.Code != 400 {
		t.Fatalf("bad value status = %d", bad.Code)
	}
}

func TestDeviceCreateKicksAutofill(t *testing.T) {
	srv, st := testServer(t)
	af := &fakeAutofill{}
	srv.autofill = af
	addAdmin(t, st)
	c := loginClient(t, srv)
	rec := c.post("/devices", url.Values{"name": {"x"}, "kind": {"other"}, "mac": {"3c:07:54:00:00:01"}})
	if rec.Code >= 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if af.kicks != 1 {
		t.Fatalf("kicks = %d", af.kicks)
	}
	_ = store.Device{}
	_ = httptest.NewRecorder
}
```

`loginClient`, `c.post`, `c.get` stand for whatever helper existing web tests use to make authenticated requests with a CSRF token (look at `settings_test.go` and `devices_test.go` and reuse theirs exactly; rename in this test to match). The device-create form fields must match `handleDeviceCreate`.

Run: `go test ./internal/web/ -run Autofill` — FAIL (`srv.autofill undefined`).

- [ ] **Step 5: Implement the web side**

`internal/web/server.go`:
- add the interface:

```go
// Autofiller fills in device details (autofill.Service). Run fills the given
// devices now; Kick asks for a full pass in the background.
type Autofiller interface {
	Run(ctx context.Context, ids ...int64) error
	Kick()
}
```

- `Options.Autofill Autofiller` with comment "Autofill fills in device details. Nil turns the hooks off (tests).";
- `Server.autofill Autofiller`, set from `o.Autofill` in `NewServer`;
- helper:

```go
// kickAutofill asks for an autofill pass, when autofill is wired in.
func (s *Server) kickAutofill() {
	if s.autofill != nil {
		s.autofill.Kick()
	}
}
```

`internal/web/devices.go`:
- in `handlePortScan`, after `ReplaceOpenPorts` succeeds:

```go
	// Open ports are evidence for autofill; run it for this device now so
	// the page the user lands on already shows what they imply.
	if s.autofill != nil {
		if err := s.autofill.Run(r.Context(), id); err != nil {
			slog.Error("autofill after port scan", "device_id", id, "err", err)
		}
	}
```

- in `handleDeviceCreate`, after the device is created successfully: `s.kickAutofill()`.

`internal/web/apiwrite.go`: after a successful device create, `s.kickAutofill()`. `internal/web/import.go`: after a committed import, `s.kickAutofill()`.

`internal/web/settings.go`:
- add `"autofill_enabled"` to the key list at line ~155 so the page gets its value;
- in `handleGeneralSave`, after the `presence_fallback` block:

```go
	if v := r.FormValue("autofill_enabled"); v != "" {
		if v != "on" && v != "off" {
			s.settingsError(w, r, tab, http.StatusBadRequest, "choose whether netis fills in device details: On or Off")
			return
		}
		if err := s.store.SetSetting(r.Context(), "autofill_enabled", v); err != nil {
			s.fail(w, r, err)
			return
		}
		s.kickAutofill()
	}
```

`internal/web/views/settings_network.templ`, in `scanningForm` inside the first `field-grid narrow`, after the presence select label:

```templ
			<label>
				Fill in device details automatically
				<select name="autofill_enabled">
					if d.Values["autofill_enabled"] != "off" {
						<option value="on" selected>On</option>
						<option value="off">Off</option>
					} else {
						<option value="on">On</option>
						<option value="off" selected>Off</option>
					}
				</select>
			</label>
```

and extend the hint paragraph below with: "With fill in device details on, netis sets vendor, model, kind, icon, function and tags from the MAC address, the hostname and open ports. It never changes a value you set."

- [ ] **Step 6: Run the tests**

Run: `make generate && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd internal/scan internal/web
git commit -m "feat(autofill): run after sweeps, integration syncs, port scans and new devices"
```

---

### Task 7: Show what netis detected on the device page; docs; e2e fixture

**Files:**
- Modify: `internal/web/devices.go` (`handleDevicePage`), `internal/web/views/device_detail.templ`, the stylesheet that holds `.dd-facts` (find with `grep -rn "dd-facts" internal/web/static`)
- Modify: `internal/web/testdata/e2e_seed.sql`, `docs/discovery.md`
- Test: `internal/web/device_detail_test.go`

**Interfaces:**
- Consumes: `autofill.Explain`, `autofill.Status*`, `store.ListHints`, `store.ListAutofill` (Tasks 2–3).
- Produces: `views.DeviceDetail.Detected []autofill.Explained` and `views.DeviceDetail.Autofilled map[string]store.AutofillRecord` (applied records whose value the device still holds, keyed by field).

- [ ] **Step 1: Write the failing test**

Append to `internal/web/device_detail_test.go` (reuse its existing helpers for creating a device and fetching the page as the admin; names below are placeholders for those helpers):

```go
func TestDevicePageShowsDetected(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	ctx := t.Context()
	id, _ := st.CreateDevice(ctx, store.Device{Name: "printer", Kind: "other", Source: "scan"})
	st.ReplaceHints(ctx, id, "hostname", []store.Hint{
		{Field: "kind", Value: "printer", Confidence: 80, Detail: "hostname BRW3C2AF4A1B2C3", SeenAt: "2026-09-29T00:00:00Z"},
		{Field: "vendor", Value: "Brother", Confidence: 80, Detail: "hostname BRW3C2AF4A1B2C3", SeenAt: "2026-09-29T00:00:00Z"},
	})
	st.ReplaceHints(ctx, id, "oui", []store.Hint{
		{Field: "kind", Value: "iot", Confidence: 40, Detail: "MAC 00:1b:a9:00:00:01", SeenAt: "2026-09-29T00:00:00Z"},
	})
	st.ApplyAutofill(ctx, id, store.AutofillChanges{Writes: []store.AutofillWrite{
		{Field: "kind", Value: "printer", Source: "hostname", Expect: "other"},
	}})
	body := getAsAdmin(t, srv, fmt.Sprintf("/devices/%d", id)) // existing helper
	for _, want := range []string{
		"What netis detected",
		"hostname BRW3C2AF4A1B2C3",
		"Detected from the hostname",   // badge title on Kind
		"Suggestion",                    // the 40-confidence oui hint
		"Not used yet",                  // vendor: resolved but pending (kept)
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}
```

Run: `go test ./internal/web/ -run Detected` — FAIL.

- [ ] **Step 2: Load the data in the handler**

In `handleDevicePage`, after `tags` are loaded:

```go
	hints, err := s.store.ListHints(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	recs, err := s.store.ListAutofill(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tagNames := make([]string, len(tags))
	for i, t := range tags {
		tagNames[i] = t.Name
	}
	autofilled := map[string]store.AutofillRecord{}
	for _, rec := range recs {
		if rec.State == store.AutofillApplied {
			autofilled[rec.Field] = rec
		}
	}
```

and pass `Detected: autofill.Explain(d, tagNames, recs, hints), Autofilled: autofilled,` in the `views.DeviceDetail` literal.

- [ ] **Step 3: Render it**

In `internal/web/views/device_detail.templ`:
- import `netis/internal/autofill`;
- add to `DeviceDetail`:

```go
	// Detected is every autofill hint for the device with what became of it.
	Detected []autofill.Explained
	// Autofilled holds the values autofill wrote that the device still
	// shows, by field (vendor, kind, …; tags as tag:<name>).
	Autofilled map[string]store.AutofillRecord
```

- helpers (Go, in the templ file's Go section or a sibling `.go` file in `views`):

```go
// sourceLabels names hint sources for people.
var sourceLabels = map[string]string{
	"oui": "the MAC vendor list", "hostname": "the hostname", "ports": "open ports",
	"mdns": "mDNS", "ssdp": "UPnP",
}

func sourceLabel(src string) string {
	if l, ok := sourceLabels[src]; ok {
		return l
	}
	return src
}

// detectedFrom returns the badge title for a field autofill filled and the
// device still shows, or "" when a person set it.
func (d DeviceDetail) detectedFrom(field string) string {
	rec, ok := d.Autofilled[field]
	if !ok || rec.Value != fieldValue(d.Device, field) {
		return ""
	}
	return "Detected from " + sourceLabel(rec.Source)
}

func fieldValue(dev store.Device, field string) string {
	switch field {
	case "vendor":
		return dev.Vendor
	case "model":
		return dev.Model
	case "kind":
		return dev.Kind
	case "icon":
		return dev.Icon
	case "function":
		return dev.Function
	case "name":
		return dev.Name
	}
	return ""
}

// hintStatus says what became of a hint. A kept hint is "already set" when
// the field holds some other value, and "not used yet" when it is empty (the
// next pass will fill it).
func (d DeviceDetail) hintStatus(e autofill.Explained) string {
	if e.Status == autofill.StatusKept && e.Field != "tag" {
		cur := fieldValue(d.Device, e.Field)
		if cur != "" && cur != e.Value && !(e.Field == "kind" && cur == "other") {
			return "Not used: already set"
		}
	}
	return statusText[e.Status]
}

var statusText = map[string]string{
	autofill.StatusApplied:   "Used",
	autofill.StatusLow:       "Suggestion",
	autofill.StatusOwned:     "Not used: you set this",
	autofill.StatusOutranked: "Not used: another clue won",
	autofill.StatusKept:      "Not used yet",
}

var fieldText = map[string]string{
	"vendor": "Vendor", "model": "Model", "kind": "Kind", "icon": "Icon",
	"function": "Function", "name": "Name", "tag": "Tag",
}
```

- a badge component:

```templ
// detectedBadge marks a value autofill filled in, naming where it came from.
templ detectedBadge(title string) {
	if title != "" {
		<span class="chip chip-detected" title={ title }>Detected</span>
	}
}
```

- in `aboutPanel` (the About `dl`): after `<dd>{ kindName(d.Device.Kind) }</dd>` put the badge inside the `dd`: `<dd>{ kindName(d.Device.Kind) } @detectedBadge(d.detectedFrom("kind"))</dd>`; same for Vendor, Model, Function.

- a panel after the About panel (inside the same column, same order on phones):

```templ
// detectedPanel lists every clue autofill has about the device and what
// became of each.
templ detectedPanel(d DeviceDetail) {
	if len(d.Detected) > 0 {
		<details class="panel dd-detected">
			<summary><h3>What netis detected</h3></summary>
			<table class="table">
				<thead>
					<tr><th scope="col">Field</th><th scope="col">Value</th><th scope="col">From</th><th scope="col">Status</th></tr>
				</thead>
				<tbody>
					for _, e := range d.Detected {
						<tr>
							<td>{ fieldText[e.Field] }</td>
							<td>{ e.Value }</td>
							<td>
								{ Sentence(sourceLabel(e.Source)) }
								if e.Detail != "" {
									<span class="muted mono">{ e.Detail }</span>
								}
							</td>
							<td>{ d.hintStatus(e) }</td>
						</tr>
					}
				</tbody>
			</table>
		</details>
	}
}
```

Call `@detectedPanel(d)` right after the About panel in the page layout. Check `Sentence` exists (it does, used for Proxmox status); if the table class differs in this codebase (grep for `<table class=` in views), use the existing one.

CSS (in the stylesheet holding `.dd-facts`), using existing tokens only:

```css
.chip-detected { font-size: var(--text-xs, .75rem); color: var(--muted); margin-left: .375rem; }
.dd-detected summary h3 { display: inline; }
.dd-detected td .mono { display: block; }
```

Use the variable names the stylesheet already uses (grep `--muted`/`--text-xs`); do not invent tokens.

- [ ] **Step 4: Run tests**

Run: `make generate && go test ./internal/web/`
Expected: PASS, including the contrast and a11y-adjacent unit tests (`contrast_test.go`).

- [ ] **Step 5: Seed the e2e fixture and re-baseline**

In `internal/web/testdata/e2e_seed.sql`, pick the fixture's printer-like or unnamed device and add hints and one applied record (plain `INSERT INTO device_hint …` / `INSERT INTO device_autofill …` rows, `seen_at`/`updated_at` `'2026-09-15T12:00:00Z'`), so the device page shows a Detected badge and the panel. Then:

Run: `make e2e-update` (needs Docker), inspect the changed images in `e2e/__screenshots__` (only that device page and nothing else should change), then `make e2e`.
Expected: PASS. If Docker is not available, say so in the task report; do not commit stale baselines.

- [ ] **Step 6: Document it**

Add to `docs/discovery.md` a section:

```markdown
## Filling in device details

netis fills in vendor, model, kind, icon, function and tags from what it can
see: the maker registered for the MAC address (the IEEE registry, built in),
the hostname (`BRW3C2AF4A1B2C3` is a Brother printer, `Galaxy-S23` a Samsung
phone) and open ports found by a port scan (9100 is a printer, 8006 Proxmox).

It only fills a field that is empty (kind counts as empty while it is Other),
and it never changes a value you set: once you edit or clear a value netis
filled, that field is yours. A tag you remove stays removed. On a device's
page, **Detected** marks values netis filled, and **What netis detected**
lists every clue and whether it was used.

Turn it off under Settings, Network, "Fill in device details automatically".
Every change is recorded in the audit log as user `netis`.

The MAC registry is refreshed with `make oui`.
```

Run: `go test ./...` (the docs link checker from d21842c runs in the suite).
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web docs e2e
git commit -m "feat(web): show detected device details and where they came from"
```

---

## Self-review notes

- Spec coverage: model (Task 2), resolver/threshold/rank (3), apply rules 1–5 and tags (3), OUI incl. MA-M/MA-S and normalisation (1), hostname and port tables (4), service, triggers, setting, audit (5–6), UI badge/panel/toggle, docs, e2e (6–7). Name hints have no A1 source; the rules and tests are in place for A2.
- The `oui` kind hint list uses normalised names; Task 4 checks them against real data.
