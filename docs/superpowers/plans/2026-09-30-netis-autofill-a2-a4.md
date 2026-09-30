# Device autofill A2-A4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add mDNS (A2) and SSDP/UPnP (A3) as autofill sources, probed per attached subnet after sweeps, and suggest values in the device form (A4).

**Architecture:** `internal/probe` sends link-local queries and returns raw observations keyed by the answering address. `internal/autofill` turns them into hints (rule tables in `probe_hints.go`), stores them per device via `ListSubnetIfaceIPs`, and runs a pass for the devices that answered. A worker in `autofill.Service` probes each subnet at most every 15 minutes after the scan engine asks. The device form gains datalists and "Use detected" buttons fed by the store and the resolver.

**Tech Stack:** Go, `golang.org/x/net/dns/dnsmessage` and `golang.org/x/net/ipv4` (already dependencies), `encoding/xml`, templ, plain JS in `internal/web/static`. Spec: `docs/superpowers/specs/2026-09-29-netis-autofill-design.md` (A2-A4 notes and "A2-A4 decisions").

## Global Constraints

- No new Go module dependencies.
- An answer belongs to the UDP source address it came from; addresses with no device in the subnet are ignored.
- Probes run only for a subnet a local interface has an address in, at most once per 15 minutes per subnet, only while `autofill_enabled` is not `off`.
- mDNS: 224.0.0.251:5353, PTR questions with the QU bit (0x8000), ephemeral source port, 3 s window. SSDP: 239.255.255.250:1900, `M-SEARCH` `ST: ssdp:all`, 3 s window.
- SSDP description fetch: only when the LOCATION host equals the responder IP; http/https only; no redirects; 2 s timeout; 64 KiB cap; at most 4 locations per responder.
- A device that does not answer keeps its previous probe hints; a device that answers has that source's hints replaced (skip the write when unchanged, as local sources do).
- Probe and hint code never fails a sweep: errors are logged.
- Hint confidences and values are exactly those in the tables below. Source names are `mdns` and `ssdp`.
- UI copy: sentence case, plain words, "Detected" not "autofilled"; existing CSS tokens and classes only.
- Commit messages: conventional prefix, ending with the session's attribution lines. Run `make generate` before `go test` when `.templ` files change.

## File map

- Create `internal/probe/iface.go` — `InterfaceFor(cidr string) *net.Interface`.
- Create `internal/probe/mdns.go`, `internal/probe/mdns_test.go` — `BrowseMDNS`.
- Create `internal/probe/ssdp.go`, `internal/probe/ssdp_test.go` — `SearchSSDP`.
- Create `internal/autofill/probe_hints.go`, `internal/autofill/probe_hints_test.go` — `mdnsHints`, `ssdpHints`, `appleModel`.
- Create `internal/autofill/probe.go`, `internal/autofill/probe_test.go` — `Service.Probe`, worker, `probeSubnet`.
- Modify `internal/autofill/service.go` (fields, Start spawns worker), `internal/scan/engine.go` (+ test), `cmd/netis/main.go` only if needed.
- Modify `internal/store/device.go` or `internal/store/autofill.go` — `DistinctDeviceValues`.
- Modify `internal/web/devices.go`, `internal/web/views/device_form.templ`, `internal/web/static/dialog.js` (or `devices.js`), `internal/web/static/pages/*.css` as needed.
- Modify `docs/discovery.md`, `docs/install.md` (multicast note), e2e baselines.

---

### Task 1: mDNS browse (`internal/probe`)

**Files:** Create `internal/probe/iface.go`, `internal/probe/mdns.go`, `internal/probe/mdns_test.go`.

**Interfaces:**
- Produces:

```go
package probe

// InterfaceFor returns the up, multicast-capable interface that has an
// address inside cidr, or nil (a routed subnet, or none matches).
func InterfaceFor(cidr string) *net.Interface

// MDNSService is one service instance a host announced.
type MDNSService struct {
	IP       string            // UDP source address of the answer
	Type     string            // service type without domain, e.g. "_googlecast._tcp"
	Instance string            // instance label, e.g. "Living Room TV"
	TXT      map[string]string // TXT key=value pairs, keys lower-cased; a bare key maps to ""
}

// MDNSTypes are the service types BrowseMDNS asks for by default.
var MDNSTypes = []string{
	"_googlecast._tcp", "_airplay._tcp", "_raop._tcp", "_device-info._tcp",
	"_ipp._tcp", "_ipps._tcp", "_printer._tcp", "_pdl-datastream._tcp",
	"_hap._tcp", "_hue._tcp", "_esphomelib._tcp", "_home-assistant._tcp",
	"_smb._tcp", "_sonos._tcp", "_spotify-connect._tcp", "_workstation._tcp",
	"_companion-link._tcp",
}

// BrowseMDNS asks the link behind ifi for types and collects answers for
// window. ifi nil sends without choosing an interface.
func BrowseMDNS(ctx context.Context, ifi *net.Interface, types []string, window time.Duration) ([]MDNSService, error)
```

Internal testable core: `browseMDNS(ctx, ifi, addr string, types []string, window time.Duration) ([]MDNSService, error)` where `addr` is `224.0.0.251:5353` in production and a local responder in tests. `BrowseMDNS` calls it with the group address.

**Behaviour:**
1. Open `net.ListenUDP("udp4", nil)`. If `addr` is multicast: wrap with `ipv4.NewPacketConn`, `SetMulticastInterface(ifi)` when `ifi != nil`, `SetMulticastTTL(255)` (same as `internal/scan/resolve.go:mdnsLookupAddr`).
2. Build one query (`dnsmessage.Builder`, header zero) with a PTR question per type, name `<type>.local.`, class `dnsmessage.ClassINET | 0x8000`. If a packet would exceed 1400 bytes, split into several queries (17 types fit in one; still guard it).
3. Read until `min(now+window, ctx deadline)`. For each packet: source IP = `*net.UDPAddr` IP string. Parse with `dnsmessage.Parser`; ignore non-responses. Walk answers and additionals (`p.AllAnswers()`, skip authorities, `p.AllAdditionals()`), collecting per source IP:
   - PTR whose name is `<type>.local.` for a requested type: instance full name = PTR target; record `(ip, fullName) → type`.
   - TXT: `(ip, name) → TXT strings`.
4. Instance label = full name with the suffix `.` + type + `.local.` removed (case-insensitive). Skip PTRs whose target lacks that suffix.
5. When a PTR arrives and no TXT for that `(ip, fullName)` has been seen by the end of processing that packet, send (once per full name) a follow-up query with a TXT question for the full name (QU bit) to `addr`.
6. At the deadline, return one `MDNSService` per `(ip, fullName)` with its TXT parsed (`key=value` split on the first `=`, key lower-cased; `key` without `=` maps to `""`). Sort by IP then type then instance for determinism.
7. Errors: socket/build errors are returned; parse errors on one packet skip that packet. A read timeout ends the loop normally.

**Tests (`mdns_test.go`)** use a fake responder: `net.ListenUDP("udp4", 127.0.0.1:0)` in a goroutine; it parses each query and replies to the querier's address with a response built by `dnsmessage.Builder` (`Header{Response: true, Authoritative: true}`).

- [ ] **Step 1: Write failing tests**

```go
package probe

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// responder answers mDNS queries on a loopback socket. For each PTR
// question it knows it answers with the PTR; with inlineTXT it adds the TXT
// as an additional record, otherwise only a later TXT question gets it.
type responder struct {
	conn      *net.UDPConn
	ptr       map[string]string   // "<type>.local." -> instance full name
	txt       map[string][]string // instance full name -> TXT strings
	inlineTXT bool
	txtAsked  chan string
}

func newResponder(t *testing.T, inline bool) *responder {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r := &responder{conn: c, ptr: map[string]string{}, txt: map[string][]string{}, inlineTXT: inline, txtAsked: make(chan string, 8)}
	t.Cleanup(func() { c.Close() })
	go r.serve()
	return r
}

func (r *responder) addr() string { return r.conn.LocalAddr().String() }

func (r *responder) serve() {
	buf := make([]byte, 9000)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var p dnsmessage.Parser
		if _, err := p.Start(buf[:n]); err != nil {
			continue
		}
		qs, _ := p.AllQuestions()
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
		b.StartAnswers()
		var extra []string
		for _, q := range qs {
			name := q.Name.String()
			switch q.Type {
			case dnsmessage.TypePTR:
				if inst, ok := r.ptr[name]; ok {
					b.PTRResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 120},
						dnsmessage.PTRResource{PTR: dnsmessage.MustNewName(inst)})
					if r.inlineTXT {
						extra = append(extra, inst)
					}
				}
			case dnsmessage.TypeTXT:
				r.txtAsked <- name
				if t, ok := r.txt[name]; ok {
					b.TXTResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 120},
						dnsmessage.TXTResource{TXT: t})
				}
			}
		}
		b.StartAdditionals()
		for _, inst := range extra {
			b.TXTResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(inst), Class: dnsmessage.ClassINET, TTL: 120},
				dnsmessage.TXTResource{TXT: r.txt[inst]})
		}
		msg, err := b.Finish()
		if err == nil {
			r.conn.WriteToUDP(msg, from)
		}
	}
}

func TestBrowseMDNSInlineTXT(t *testing.T) {
	r := newResponder(t, true)
	r.ptr["_googlecast._tcp.local."] = "Living Room TV._googlecast._tcp.local."
	r.txt["Living Room TV._googlecast._tcp.local."] = []string{"md=Chromecast", "fn=Living Room TV", "RS"}
	got, err := browseMDNS(context.Background(), nil, r.addr(), []string{"_googlecast._tcp", "_ipp._tcp"}, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("services = %+v", got)
	}
	s := got[0]
	if s.IP != "127.0.0.1" || s.Type != "_googlecast._tcp" || s.Instance != "Living Room TV" ||
		s.TXT["md"] != "Chromecast" || s.TXT["fn"] != "Living Room TV" {
		t.Fatalf("service = %+v", s)
	}
	if v, ok := s.TXT["rs"]; !ok || v != "" {
		t.Fatalf("bare key: %+v", s.TXT)
	}
}

func TestBrowseMDNSFollowsUpForTXT(t *testing.T) {
	r := newResponder(t, false)
	r.ptr["_ipp._tcp.local."] = "Brother HL-L2350DW._ipp._tcp.local."
	r.txt["Brother HL-L2350DW._ipp._tcp.local."] = []string{"ty=Brother HL-L2350DW series", "usb_MFG=Brother"}
	got, err := browseMDNS(context.Background(), nil, r.addr(), []string{"_ipp._tcp"}, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TXT["ty"] != "Brother HL-L2350DW series" || got[0].TXT["usb_mfg"] != "Brother" {
		t.Fatalf("services = %+v", got)
	}
	select {
	case name := <-r.txtAsked:
		if !strings.HasPrefix(name, "Brother HL-L2350DW.") {
			t.Fatalf("asked TXT for %q", name)
		}
	default:
		t.Fatal("no follow-up TXT question")
	}
}

func TestBrowseMDNSNothingAnswers(t *testing.T) {
	r := newResponder(t, true) // knows nothing
	got, err := browseMDNS(context.Background(), nil, r.addr(), MDNSTypes, 200*time.Millisecond)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestInterfaceForLoopback(t *testing.T) {
	// Loopback is not multicast-capable on most systems, so nothing matches
	// 127.0.0.0/8; an unparsable CIDR is nil too.
	if InterfaceFor("not a cidr") != nil {
		t.Fatal("bad cidr matched")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/probe/` — FAIL (undefined `browseMDNS`).
- [ ] **Step 3: Implement** `iface.go` (parse with `netip.ParsePrefix`; loop `net.Interfaces()` up + multicast; any `*net.IPNet` address whose IP the prefix contains) and `mdns.go` per the behaviour above. Keep functions small: `buildQuestions(types) ([][]byte, error)`, `buildTXTQuery(name) ([]byte, error)`, `parseTXT([]string) map[string]string`, and the read loop.
- [ ] **Step 4: Run** `go test -race ./internal/probe/` — PASS. `go vet ./internal/probe/`.
- [ ] **Step 5: Commit** `feat(probe): browse mDNS services on a link`.

---

### Task 2: SSDP search and UPnP description (`internal/probe`)

**Files:** Create `internal/probe/ssdp.go`, `internal/probe/ssdp_test.go`.

**Interfaces:**

```go
// UPnPDevice is a root device a host described over UPnP.
type UPnPDevice struct {
	IP           string // responder address
	Location     string
	DeviceType   string // e.g. "urn:schemas-upnp-org:device:MediaRenderer:1"
	FriendlyName string
	Manufacturer string
	ModelName    string
	ModelNumber  string
}

// SearchSSDP sends an M-SEARCH out of ifi and returns the root devices whose
// descriptions could be fetched, one per (IP, Location).
func SearchSSDP(ctx context.Context, ifi *net.Interface, window time.Duration) ([]UPnPDevice, error)
```

Testable core: `searchSSDP(ctx, ifi, addr string, window time.Duration, client *http.Client) ([]UPnPDevice, error)`. Production: `addr = "239.255.255.250:1900"`, client = `ssdpClient()`:

```go
func ssdpClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
```

**Behaviour:**
1. UDP4 ephemeral socket; multicast interface/TTL (TTL 2 is fine for SSDP; use 2) as in Task 1.
2. Send, twice 100 ms apart (UDP loss), the request:

```
M-SEARCH * HTTP/1.1\r\n
HOST: 239.255.255.250:1900\r\n
MAN: "ssdp:discover"\r\n
MX: 2\r\n
ST: ssdp:all\r\n
\r\n
```

3. Read responses until `window`. Parse each as HTTP response headers (`http.ReadResponse(bufio.NewReader(bytes.NewReader(pkt)), nil)`); take `LOCATION`. Keep unique `(srcIP, location)`, at most 4 per srcIP.
4. For each kept pair, in parallel (bounded to 8): skip unless `url.Parse` gives scheme http/https and `u.Hostname() == srcIP`. GET with ctx; skip non-200; read `io.LimitReader(body, 64<<10)`; decode:

```go
type upnpRoot struct {
	Device struct {
		DeviceType   string `xml:"deviceType"`
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		ModelNumber  string `xml:"modelNumber"`
	} `xml:"device"`
}
```

   Trim spaces in every field. A device with all of DeviceType, Manufacturer, ModelName, FriendlyName empty is dropped.
5. Return sorted by IP then Location. Fetch errors skip that location (no error return). Socket errors are returned.

- [ ] **Step 1: Write failing tests** (`ssdp_test.go`):

```go
package probe

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const descXML = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><device>
<deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
<friendlyName> Kitchen speaker </friendlyName>
<manufacturer>Sonos, Inc.</manufacturer>
<modelName>Sonos One</modelName>
<modelNumber>S18</modelNumber>
</device></root>`

// ssdpResponder answers any M-SEARCH with one response per location.
func ssdpResponder(t *testing.T, locations ...string) string {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf[:n])))
			if err != nil || req.Method != "M-SEARCH" {
				continue
			}
			for _, loc := range locations {
				resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nST: upnp:rootdevice\r\nLOCATION: %s\r\n\r\n", loc)
				c.WriteToUDP([]byte(resp), from)
			}
		}
	}()
	return c.LocalAddr().String()
}

func TestSearchSSDPFetchesDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, descXML)
	}))
	defer srv.Close()
	addr := ssdpResponder(t, srv.URL+"/desc.xml")
	got, err := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("devices = %+v", got)
	}
	d := got[0]
	if d.IP != "127.0.0.1" || d.FriendlyName != "Kitchen speaker" || d.Manufacturer != "Sonos, Inc." ||
		d.ModelName != "Sonos One" || d.ModelNumber != "S18" || !strings.HasSuffix(d.DeviceType, "MediaRenderer:1") {
		t.Fatalf("device = %+v", d)
	}
}

func TestSearchSSDPOnlyFetchesFromResponder(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, descXML)
	}))
	defer srv.Close()
	// Same server, but named "localhost": not the responder's address.
	other := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	addr := ssdpResponder(t, other+"/desc.xml", "file:///etc/passwd")
	got, _ := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if len(got) != 0 || hits.Load() != 0 {
		t.Fatalf("fetched from elsewhere: devices=%+v hits=%d", got, hits.Load())
	}
}

func TestSearchSSDPCapsBodyAndIgnoresRedirects(t *testing.T) {
	big := strings.Repeat("x", 70<<10)
	mux := http.NewServeMux()
	mux.HandleFunc("/big.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<root><device><friendlyName>`+big+`</friendlyName></device></root>`)
	})
	mux.HandleFunc("/moved.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/desc.xml", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	addr := ssdpResponder(t, srv.URL+"/big.xml", srv.URL+"/moved.xml")
	got, _ := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if len(got) != 0 {
		t.Fatalf("devices = %+v", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/probe/ -run SSDP` — FAIL.
- [ ] **Step 3: Implement** `ssdp.go` per the behaviour. A truncated body fails XML decoding and is skipped, which is what the cap test expects.
- [ ] **Step 4: Run** `go test -race ./internal/probe/` — PASS; `go vet`.
- [ ] **Step 5: Commit** `feat(probe): find UPnP devices over SSDP`.

---

### Task 3: Hints from mDNS and UPnP (`internal/autofill/probe_hints.go`)

**Files:** Create `internal/autofill/probe_hints.go`, `internal/autofill/probe_hints_test.go`.

**Interfaces:**
- Consumes: `probe.MDNSService`, `probe.UPnPDevice` (Tasks 1-2), `hint()`, `dedupe()`, field constants (A1), `oui.Normalize`.
- Produces: `mdnsHints(svcs []probe.MDNSService) []store.Hint` (all services of one address), `ssdpHints(devs []probe.UPnPDevice) []store.Hint` (all devices of one address), `appleModel(id string) string`.

Hints carry Field, Value, Confidence, Detail; Detail for mDNS is `mDNS <type> "<instance>"` (e.g. `mDNS _googlecast._tcp "Living Room TV"`); for UPnP `UPnP <deviceType short> "<friendlyName>"` (short = the part after `device:` up to the next `:`, e.g. `MediaRenderer`). Empty TXT values produce no hint. Names (instance, fn, friendly_name, friendlyName) are trimmed; a name hint is only emitted when non-empty.

**mDNS table** (`t` = TXT):

| Type | Hints |
|---|---|
| `_googlecast._tcp` | model `t[md]` 80; name `t[fn]` (else instance) 70; icon tv 60; tag media 60 |
| `_airplay._tcp` | model `appleModel(t[model])` 70; vendor Apple 70 when `t[model]` has an Apple prefix (see below); icon tv 70 when model starts `AppleTV`, icon speaker 70 when `AudioAccessory`; name instance 60 |
| `_raop._tcp` | model `appleModel(t[am])` 60; name = part of instance after `@` 50 |
| `_device-info._tcp` | model `appleModel(t[model])` 70; when Apple prefix: vendor Apple 70; kind computer 60 for Mac prefixes (`MacBook`, `iMac`, `Macmini`, `MacPro`, `Mac`) |
| `_companion-link._tcp` | vendor Apple 70; model `appleModel(t[rpmd])` 70; kind phone 70 when rpmd starts `iPhone` or `iPad`; icon tablet 70 for `iPad` |
| `_ipp._tcp`, `_ipps._tcp`, `_printer._tcp`, `_pdl-datastream._tcp` | kind printer 90; model `t[usb_mdl]` 80 else `t[ty]` 75; vendor `oui.Normalize(t[usb_mfg])` 80; name instance 60 |
| `_hap._tcp` | kind iot 70; model `t[md]` 70; icon from `t[ci]` 70 (below); tag smart-home 60; name instance 60 |
| `_hue._tcp` | vendor Philips Hue 80; kind iot 80; model Hue Bridge 70; tag smart-home 60 |
| `_esphomelib._tcp` | kind iot 80; function ESPHome 70; tag smart-home 60; name `t[friendly_name]` (else instance) 60 |
| `_home-assistant._tcp` | kind server 60; function Home Assistant 90; tag smart-home 60 |
| `_smb._tcp` | tag file-share 50 |
| `_sonos._tcp` | vendor Sonos 90; icon speaker 80; tag media 60 |
| `_spotify-connect._tcp` | tag media 50 |
| `_workstation._tcp` | kind computer 60; name = instance with a trailing ` [xx:xx:xx:xx:xx:xx]` removed, 50 |

HomeKit `ci` → icon: `5` lightbulb, `7` plug, `8` plug, `9` thermometer, `10` thermometer, `17` cctv, `26` speaker, `31` tv, `32` tv; others none.

`appleModel(id)`: families by prefix, longest first — `MacBookPro` → `MacBook Pro`, `MacBookAir` → `MacBook Air`, `MacBook` → `MacBook`, `iMac` → `iMac`, `Macmini` → `Mac mini`, `MacPro` → `Mac Pro`, `AppleTV` → `Apple TV`, `AudioAccessory` → `HomePod`, `iPhone` → `iPhone`, `iPad` → `iPad`, `Watch` → `Apple Watch`. Result `Family (id)`, e.g. `MacBook Pro (MacBookPro18,3)`. Prefix must be followed by a digit (`MacBookPro18,3` yes, `MacBookProMax` no). `Mac14,2` and unknown ids return `id` unchanged; empty returns empty. "Apple prefix" = any of those prefixes or `Mac` followed by a digit.

**UPnP table** (per device):

| Field | Hint |
|---|---|
| Manufacturer | vendor `oui.Normalize(m)` 80 |
| ModelName (+ ` ` + ModelNumber when non-empty and not already contained) | model 80 |
| FriendlyName | name 70 |
| deviceType `InternetGatewayDevice` | kind router 80 |
| `WLANAccessPointDevice` | kind router 60 |
| `MediaRenderer` | icon tv 60; tag media 60 |
| `MediaServer` | tag media 50 |
| `Printer` | kind printer 80 |
| `DigitalSecurityCamera` | icon cctv 70; tag camera 60 |
| `ZonePlayer` | icon speaker 70; tag media 60 |
| deviceType contains `roku-com` | icon tv 70; vendor Roku 70; tag media 60 |

`Basic` and unknown types add nothing beyond the Manufacturer/Model/Name rows.

- [ ] **Step 1: Write failing tests** covering at least: googlecast (model, name from fn, icon, tag); airplay with `model=AppleTV6,2` (model `Apple TV (AppleTV6,2)`, vendor Apple, icon tv); device-info `MacBookPro18,3` (kind computer, vendor Apple); companion-link `rpMd=iPad13,4` (kind phone, icon tablet) — note TXT keys are lower-cased by the probe, so the test passes `rpmd`; ipp with usb_MDL beating ty, vendor normalised from `Brother Industries, Ltd.` → `Brother`; hap `ci=5` → icon lightbulb; workstation name `nas [aa:bb:cc:dd:ee:ff]` → `nas`; raop `A1B2C3D4E5F6@Kitchen` → name `Kitchen`; empty TXT values produce nothing; UPnP Sonos description → vendor `Sonos`, model `Sonos One S18`, name, icon tv (MediaRenderer), tag media; IGD → kind router; model number already in model name not duplicated; `appleModel` table (`MacBookPro18,3`, `Macmini9,1`, `Mac14,2`, `MacBookProMax`, `""`). Write them as table tests using the `has(hints, field, value, conf)` helper already in `sources_test.go`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** with a `map[string]func(probe.MDNSService) []store.Hint` keyed by type and small helper funcs; `dedupe` the combined result.
- [ ] **Step 4: Run** `go test ./internal/autofill/` — PASS.
- [ ] **Step 5: Commit** `feat(autofill): hints from mDNS services and UPnP descriptions`.

---

### Task 4: Probe worker, engine hook, docs

**Files:** Create `internal/autofill/probe.go`, `internal/autofill/probe_test.go`. Modify `internal/autofill/service.go`, `internal/scan/engine.go`, `internal/scan/engine_test.go`, `docs/discovery.md`, `docs/install.md`.

**Interfaces:**
- Consumes: `probe.BrowseMDNS`, `probe.MDNSTypes`, `probe.SearchSSDP`, `probe.InterfaceFor`, `mdnsHints`, `ssdpHints`, `hintsEqual`, `Service.Run`, `store.ListSubnetIfaceIPs`, `store.ListHints`, `store.ReplaceHints`.
- Produces:

```go
// SubnetProber finds hints on one link, keyed by the answering address.
type SubnetProber func(ctx context.Context, ifi *net.Interface) (map[string][]store.Hint, error)

// Probe asks for sn to be probed soon. It never blocks; a full queue drops
// the request (the next sweep asks again).
func (s *Service) Probe(sn store.Subnet)
```

Service gains unexported fields: `probeCh chan store.Subnet` (buffer 16), `probers map[string]SubnetProber` (`"mdns"`, `"ssdp"` set in `New`), `probedAt map[int64]time.Time` (worker-only, no lock needed), `probeEvery time.Duration` (15 min), `iface func(cidr string) *net.Interface` (default `probe.InterfaceFor`; tests override). `Start` launches the worker goroutine (`go s.probeLoop(ctx)`) before its first pass, and returns only after the worker has exited (use a `sync.WaitGroup` or done channel) so shutdown waits for it.

Default probers (in `probe.go`):

```go
func mdnsProber(ctx context.Context, ifi *net.Interface) (map[string][]store.Hint, error) {
	svcs, err := probe.BrowseMDNS(ctx, ifi, probe.MDNSTypes, 3*time.Second)
	if err != nil {
		return nil, err
	}
	byIP := map[string][]probe.MDNSService{}
	for _, sv := range svcs {
		byIP[sv.IP] = append(byIP[sv.IP], sv)
	}
	out := make(map[string][]store.Hint, len(byIP))
	for ip, list := range byIP {
		if hs := mdnsHints(list); len(hs) > 0 {
			out[ip] = hs
		}
	}
	return out, nil
}
```

and `ssdpProber` likewise over `probe.SearchSSDP(ctx, ifi, 3*time.Second)` and `ssdpHints`.

`probeLoop(ctx)`: for each subnet received: skip if `!Enabled`, if `probedAt[sn.ID]` is within `probeEvery` of `s.now()`, or if `s.iface(sn.CIDR) == nil`. Else set `probedAt[sn.ID] = s.now()` and call `s.probeSubnet(ctx, sn, ifi)`, logging its error.

`probeSubnet(ctx, sn, ifi) error`:
1. Run every prober concurrently with a 10 s timeout context; collect `source → map[ip][]Hint`; a failing prober is logged and skipped.
2. `ListSubnetIfaceIPs(sn.ID)` → `ip → deviceID` (first wins).
3. For each source and each ip with a device: stamp DeviceID/Source; compare with the device's stored hints of that source (`ListHints` filtered) using `hintsEqual`; if different, stamp `SeenAt = s.now()` and `ReplaceHints`. Record the device id as touched either way.
4. `s.Run(ctx, touched...)` when any.

**Engine hook:** in `internal/scan/engine.go` change the field type to

```go
// Autofill fills in device details after a sweep: Kick asks for a pass,
// Probe asks for the swept subnet's link to be probed. Nil turns both off.
Autofill Autofiller
```

with `type Autofiller interface { Kick(); Probe(sn store.Subnet) }` (replacing `Kicker`), and at the end of `RunSubnet`: `e.Autofill.Probe(sn)` then `e.Autofill.Kick()`. Update `countKicker` in `engine_test.go` to record probes and assert one probe for the swept subnet.

- [ ] **Step 1: Write failing tests** (`probe_test.go`, package autofill, `storetest.EachDialect`):
  - `TestProbeSubnetStoresHintsAndFills`: subnet 10.0.0.0/24 with a discovered device at 10.0.0.5 (reuse `discovered` from service_test.go); a Service whose `probers` is `{"mdns": fake}` returning `{"10.0.0.5": [{Field:"kind",Value:"printer",Confidence:90}], "10.0.0.99": [...]}`; call `probeSubnet` directly with ifi nil; assert the device's kind is printer, `ListHints` has one `mdns` hint, and nothing was stored for 10.0.0.99 (no device).
  - `TestProbeSubnetSkipsUnchanged`: probe twice with the same fake and a controllable `now`; `seen_at` of the mdns hint stays at the first time.
  - `TestProbeLoopRateLimitsAndSkipsRouted`: Service with fake prober counting calls, `iface` returning a non-nil `&net.Interface{Name:"test"}` for "10.0.0.0/24" and nil otherwise; start `Start` with a cancelable ctx; `Probe` the attached subnet 3 times and a routed subnet once; wait (poll ≤ 5 s) for one call; sleep 50 ms; assert exactly 1 call; cancel and ensure `Start` returns.
  - `TestProbeOffDoesNothing`: `autofill_enabled=off` → fake never called.
  - engine: `TestSweepKicksAutofill` updated to assert one Probe with the subnet's ID.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `probe.go`, the Service fields/`New` defaults/`Start` changes, the engine interface. `Service` must satisfy `scan.Autofiller` (add `var _ interface{ Kick(); Probe(store.Subnet) } = (*Service)(nil)` in probe.go). `cmd/netis/main.go` needs no change unless the compiler says so.
- [ ] **Step 4: Docs.** `docs/discovery.md` "Filling in device details": add that netis also listens for what devices announce on the local network (mDNS/Bonjour: Chromecasts, AirPlay, printers, HomeKit, ESPHome; UPnP: TVs, speakers, routers) on subnets it is directly attached to, at most every 15 minutes, and that this needs host networking (already required, see install). `docs/install.md`: in the `--network host` note, add that it also lets netis hear mDNS and UPnP answers used to fill in device details.
- [ ] **Step 5: Run** `go vet ./... && go test -race ./internal/autofill/ ./internal/scan/ && go test ./...` — PASS. Postgres if Docker: `make pg`, `NETIS_TEST_PG_DSN=postgres://netis:netis@127.0.0.1:55432/netis?sslmode=disable go test ./internal/autofill/ ./internal/store/...`, `make pg-stop`.
- [ ] **Step 6: Commit** `feat(autofill): probe attached subnets over mDNS and UPnP after sweeps`.

---

### Task 5: Suggestions in the device form (A4)

**Files:** Modify `internal/store/autofill.go` (+ test), `internal/web/devices.go` (`deviceFormLists` or `renderDeviceForm`), `internal/web/views/device_form.templ`, `internal/web/static/dialog.js` (or the script that already handles the device form, e.g. the icon picker — grep `iconpick`), CSS page file for the form if needed; tests in `internal/web/device_detail_test.go` or a new `internal/web/device_form_test.go`; e2e baselines; `docs/usage.md` or `docs/discovery.md` one sentence.

**Interfaces:**

```go
// DistinctDeviceValues returns the distinct non-empty values of a device
// column (vendor, model or function), sorted, at most 200.
func (s *Store) DistinctDeviceValues(ctx context.Context, field string) ([]string, error)
```

`views.DeviceForm` gains:

```go
	// Suggest holds datalist values per field: vendor, model, function.
	Suggest map[string][]string
	// Detected holds, on the edit form, the value autofill resolved for a
	// field (vendor, model, function, kind) when it differs from the
	// device's current value.
	Detected map[string]string
	// DetectedTags are resolved tags the device does not have.
	DetectedTags []string
```

**Behaviour:**
- `DistinctDeviceValues` uses the same column allowlist as `ApplyAutofill` (`autofillColumns`), restricted to vendor/model/function; other fields are an error. SQL: `SELECT DISTINCT <col> FROM device WHERE <col> <> '' ORDER BY <col> LIMIT 200`.
- In the form handler path (`deviceFormLists`, used by new and edit), fill `Suggest` for the three fields. On edit (`f.IsEdit && f.Device.ID != 0`): `ListHints` → add every hint value for vendor/model/function to that field's suggestions (dedupe, keep sorted); `autofill.Resolve(hints)` → for vendor, model, function, kind: when a candidate exists and `candidate.Value != current`, set `Detected[field]`; tags: resolved tag candidates not in `f.Tags` → `DetectedTags`.
- Template: `list="dl-vendor"` etc. on the three inputs, with `<datalist id="dl-vendor">` elements rendered inside the form (ids unique on the page — the device form is the only one open at a time). Under an input whose field has `Detected`, render:

```templ
<button type="button" class="btn-link df-use" data-fill="vendor" data-value={ v }>Use detected: { v }</button>
```

  For kind, place it under the Kind select, `data-fill="kind"`, label with `kindName(v)`. For tags, under the tags input, one button per tag: `data-append-tag={ t }`, label "Add detected tag: { t }".
- JS (event delegation on `document`, so it works in the htmx-swapped modal): click on `[data-fill]` sets `form.elements[name].value = value` and dispatches `input`/`change` events (the kind select's change handler updates the icon preview); click on `[data-append-tag]` appends `, tag` (or `tag` when empty) to the tags input unless already present. Hide the clicked button after use.
- Viewer role never sees the form; nothing to gate.
- Copy: "Use detected: …", "Add detected tag: …".

- [ ] **Step 1: Write failing tests**
  - store (`EachDialect`): two devices with vendors Apple, Brother, one empty → `DistinctDeviceValues("vendor")` = [Apple Brother]; `"notes"` → error.
  - web: create a device with vendor "Mine", model "", hints (hostname) vendor Brother 80, model HL-L2350 80, tag nas 60; GET the edit form (find the route: `GET /devices/{id}/edit`) as admin; body contains `id="dl-vendor"`, `<option value="Mine">` or `value="Brother"` in the datalist, `data-fill="vendor"`, `data-value="Brother"`, `Use detected: Brother`, `data-fill="model"`, `Add detected tag: nas`. New form (`GET /devices/new`) contains the datalists but no `Use detected`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** store method, handler, template, JS, minimal CSS (`.df-use` spacing using existing tokens; reuse an existing link-button class if one exists — grep `btn-link`).
- [ ] **Step 4: Run** `make generate && go vet ./... && go test ./...` — PASS. Then `make e2e-update`; only screenshots of the device form / device edit dialog should change (inspect them; view one with the Read tool); `make e2e` — PASS.
- [ ] **Step 5: Commit** `feat(web): suggest detected values in the device form`.

---

## Self-review notes

- Spec coverage: A2 mDNS (Tasks 1, 3, 4), A3 SSDP with fetch guards (2, 3, 4), A4 datalists, Use detected, tags (5); per-subnet scheduling, rate limit, attached-only, enabled check (4); docs (4, 5).
- `hintsEqual` and `dedupe` exist from A1 (`internal/autofill/service.go`, `sources.go`).
