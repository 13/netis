package opnsense

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

const (
	keaLeasesJSON = `{"total":3,"rowCount":3,"current":1,"rows":[
	  {"address":"10.0.0.110","hwaddr":"AA:BB:CC:00:00:10","hostname":"laptop","state":"0","if_name":"lan"},
	  {"address":"10.0.0.111","hwaddr":"aa:bb:cc:00:00:11","hostname":"gone","state":2},
	  {"address":"10.0.0.112","hw_address":"aa:bb:cc:00:00:12","hostname":"","state":0}
	]}`
	keaResJSON = `{"rows":[{"hw_address":"aa:bb:cc:00:00:20","ip_address":"10.0.0.20","hostname":"printer"}]}`
	iscJSON    = `{"total":4,"rowCount":4,"current":1,"rows":[
	  {"address":"10.0.0.120","mac":"aa:bb:cc:00:00:30","hostname":"tv","type":"dynamic","state":"active"},
	  {"address":"10.0.0.121","mac":"aa:bb:cc:00:00:31","hostname":"old","type":"dynamic","state":"free"},
	  {"address":"10.0.0.30","mac":"aa:bb:cc:00:00:32","hostname":"nas","type":"static","state":""},
	  {"address":"bogus","mac":"aa:bb:cc:00:00:33","hostname":"x","type":"dynamic","state":"active"}
	]}`
	dnsmasqJSON = `{"rows":[{"address":"10.0.0.140","hwaddr":"aa:bb:cc:00:00:40","hostname":"cam"}]}`
	emptyRows   = `{"total":0,"rowCount":0,"current":1,"rows":[]}`
)

// fake serves the given bodies by path (without query), answering the status
// in codes instead where one is set, and 404 for anything else. Every request
// must carry basic auth key/secret.
type fake struct {
	bodies map[string]string
	codes  map[string]int
	hits   []string
}

func (f *fake) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits = append(f.hits, r.URL.Path)
		if u, p, ok := r.BasicAuth(); !ok || u != "key" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if c, ok := f.codes[r.URL.Path]; ok {
			w.WriteHeader(c)
			return
		}
		body, ok := f.bodies[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errorMessage":"Endpoint not found"}`))
			return
		}
		if strings.Contains(r.URL.Path, "search") && r.URL.Query().Get("rowCount") != "-1" {
			t.Errorf("%s asked without rowCount=-1: %s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const (
	keaPath     = "/api/kea/leases4/search"
	keaResPath  = "/api/kea/dhcpv4/searchReservation"
	iscPath     = "/api/dhcpv4/leases/searchLease"
	dnsmasqPath = "/api/dnsmasq/leases/search"
	arpPath     = "/api/diagnostics/interface/getArp"
)

func TestLeasesKea(t *testing.T) {
	f := &fake{bodies: map[string]string{keaPath: keaLeasesJSON, keaResPath: keaResJSON, iscPath: iscJSON}}
	c := NewClient(f.server(t).URL+"/", "key", "secret", false)
	defer c.Close()
	l, err := c.Leases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l.Backend != "kea" || len(l.Dynamic) != 2 || len(l.Static) != 1 {
		t.Fatalf("leases = %+v", l)
	}
	if d := l.Dynamic[0]; d.MAC != "aa:bb:cc:00:00:10" || d.IP != "10.0.0.110" || d.Hostname != "laptop" {
		t.Fatalf("dynamic[0] = %+v", d)
	}
	if d := l.Dynamic[1]; d.MAC != "aa:bb:cc:00:00:12" {
		t.Fatalf("hw_address spelling not read: %+v", d)
	}
	if s := l.Static[0]; s.MAC != "aa:bb:cc:00:00:20" || s.IP != "10.0.0.20" || s.Hostname != "printer" {
		t.Fatalf("static = %+v", s)
	}
	for _, h := range f.hits {
		if h == iscPath {
			t.Fatal("ISC asked although Kea had leases")
		}
	}
}

// Kea's API answers on every current install, empty when ISC serves DHCP, so
// an empty Kea moves on to ISC; a missing reservation endpoint is not fatal.
func TestLeasesFallsBackToISC(t *testing.T) {
	f := &fake{bodies: map[string]string{keaPath: emptyRows, iscPath: iscJSON}}
	c := NewClient(f.server(t).URL, "key", "secret", false)
	l, err := c.Leases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l.Backend != "isc" || len(l.Dynamic) != 1 || len(l.Static) != 1 {
		t.Fatalf("leases = %+v", l)
	}
	if l.Dynamic[0].IP != "10.0.0.120" || l.Static[0].Hostname != "nas" {
		t.Fatalf("leases = %+v", l)
	}
}

// Kea not installed (404) and ISC not granted to the key (403): Dnsmasq's
// leases are used.
func TestLeasesSkipsMissingAndForbidden(t *testing.T) {
	f := &fake{bodies: map[string]string{dnsmasqPath: dnsmasqJSON}, codes: map[string]int{iscPath: 403}}
	c := NewClient(f.server(t).URL, "key", "secret", false)
	l, err := c.Leases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l.Backend != "dnsmasq" || len(l.Dynamic) != 1 || l.Dynamic[0].Hostname != "cam" {
		t.Fatalf("leases = %+v", l)
	}
}

func TestLeasesAllEmpty(t *testing.T) {
	f := &fake{bodies: map[string]string{keaPath: emptyRows, iscPath: emptyRows}}
	c := NewClient(f.server(t).URL, "key", "secret", false)
	l, err := c.Leases(context.Background())
	if err != nil || l.Backend != "kea" || len(l.Dynamic)+len(l.Static) != 0 {
		t.Fatalf("leases = %+v err=%v", l, err)
	}
}

func TestLeasesErrors(t *testing.T) {
	// No backend available to the key: the first skip error is returned, in
	// the HTTP <code> shape the runner categorises.
	f := &fake{codes: map[string]int{keaPath: 403, iscPath: 403, dnsmasqPath: 403}}
	_, err := NewClient(f.server(t).URL, "key", "secret", false).Leases(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want HTTP 403", err)
	}
	// A bad key fails at once.
	f = &fake{bodies: map[string]string{iscPath: iscJSON}}
	_, err = NewClient(f.server(t).URL, "key", "wrong", false).Leases(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v, want HTTP 401", err)
	}
	if len(f.hits) != 1 {
		t.Fatalf("kept trying after a 401: %v", f.hits)
	}
	// A server error is not mistaken for a missing backend.
	f = &fake{codes: map[string]int{keaPath: 500}, bodies: map[string]string{iscPath: iscJSON}}
	if _, err = NewClient(f.server(t).URL, "key", "secret", false).Leases(context.Background()); err == nil {
		t.Fatal("HTTP 500 swallowed")
	}
}

func TestARP(t *testing.T) {
	for name, body := range map[string]string{
		"array": `[{"mac":"AA:BB:CC:00:01:01","ip":"10.1.0.5","intf":"igb1","expired":false,"permanent":false},
		           {"mac":"aa:bb:cc:00:01:02","ip":"10.1.0.6","expired":true},
		           {"mac":"(incomplete)","ip":"10.1.0.7","expired":false}]`,
		"rows": `{"rows":[{"mac":"AA:BB:CC:00:01:01","ip":"10.1.0.5","expired":"0"},
		           {"mac":"aa:bb:cc:00:01:02","ip":"10.1.0.6","expired":"1"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := &fake{bodies: map[string]string{arpPath: body}}
			arp, err := NewClient(f.server(t).URL, "key", "secret", false).ARP(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(arp) != 2 || arp[0].MAC != "aa:bb:cc:00:01:01" || arp[0].IP != "10.1.0.5" || arp[0].Expired || !arp[1].Expired {
				t.Fatalf("arp = %+v", arp)
			}
		})
	}
}

func strp(s string) *string { return &s }

// newSync is a store with a routed 10.1.0.0/24 subnet and a sync against f.
func newSync(t *testing.T, st *store.Store, f *fake) (*Sync, int64) {
	t.Helper()
	snID, err := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.1.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(f.server(t).URL, "key", "secret", false)
	return NewSync(st, c, events.NewService(st, events.NewBroker())), snID
}

// discovered adds a scan-found device at ip, with mac or none.
func discovered(t *testing.T, st *store.Store, snID int64, name, ip string, mac *string) int64 {
	t.Helper()
	_, ifID, err := st.CreateDiscoveredDevice(t.Context(), store.Device{Name: name, Kind: "other", Source: "scan"},
		mac, nil, snID, ip, "dhcp")
	if err != nil {
		t.Fatal(err)
	}
	return ifID
}

// On a routed subnet the scanner found hosts without MACs. The firewall's ARP
// table fills them in, and a lease for the same host then enriches that
// device instead of creating a second one.
func TestSyncFillsMACsFromARP(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		f := &fake{bodies: map[string]string{
			keaPath: `{"rows":[{"address":"10.1.0.5","hwaddr":"aa:bb:cc:00:01:01","hostname":"nas","state":"0"}]}`,
			arpPath: `[
			  {"mac":"aa:bb:cc:00:01:01","ip":"10.1.0.5","expired":false},
			  {"mac":"aa:bb:cc:00:01:02","ip":"10.1.0.6","expired":true},
			  {"mac":"aa:bb:cc:00:01:03","ip":"10.1.0.7","expired":false},
			  {"mac":"aa:bb:cc:00:01:03","ip":"10.1.0.8","expired":false},
			  {"mac":"aa:bb:cc:00:01:04","ip":"10.1.0.9","expired":false},
			  {"mac":"aa:bb:cc:00:01:05","ip":"10.1.0.10","expired":false},
			  {"mac":"aa:bb:cc:00:01:06","ip":"10.1.0.11","expired":false},
			  {"mac":"aa:bb:cc:00:01:07","ip":"10.1.0.12","expired":false}
			]`,
		}}
		sync, snID := newSync(t, st, f)
		nas := discovered(t, st, snID, "unknown-10.1.0.5", "10.1.0.5", nil)
		expired := discovered(t, st, snID, "e", "10.1.0.6", nil)
		proxied := discovered(t, st, snID, "p", "10.1.0.7", nil)
		hasMAC := discovered(t, st, snID, "m", "10.1.0.9", strp("aa:bb:cc:00:09:99"))
		// A MAC another interface already has is not handed to a second one.
		discovered(t, st, snID, "elsewhere", "10.1.0.200", strp("aa:bb:cc:00:01:05"))
		taken := discovered(t, st, snID, "t", "10.1.0.10", nil)
		// Two interfaces claim 10.1.0.11: which one ARP means is unknown.
		conflictA := discovered(t, st, snID, "c1", "10.1.0.11", nil)
		devB, _ := st.CreateDevice(ctx, store.Device{Name: "c2", Kind: "other", Source: "manual"})
		conflictB, _ := st.AddIface(ctx, devB, nil, nil)
		st.AssignIP(ctx, conflictB, snID, "10.1.0.11", "static")

		stats, err := sync.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if stats.MACsFilled != 1 || stats.Created != 0 || stats.ARPUnavailable {
			t.Fatalf("stats = %+v", stats)
		}
		macOf := func(ifID int64) string {
			t.Helper()
			rows, err := st.ListSubnetIfaceIPs(ctx, snID)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.IfaceID == ifID && r.MAC != nil {
					return *r.MAC
				}
			}
			return ""
		}
		if got := macOf(nas); got != "aa:bb:cc:00:01:01" {
			t.Fatalf("nas MAC = %q", got)
		}
		for name, id := range map[string]int64{"expired": expired, "proxy-arp": proxied, "taken": taken,
			"conflict a": conflictA, "conflict b": conflictB} {
			if got := macOf(id); got != "" {
				t.Errorf("%s iface got MAC %q", name, got)
			}
		}
		if got := macOf(hasMAC); got != "aa:bb:cc:00:09:99" {
			t.Errorf("set MAC changed to %q", got)
		}
		// The lease matched the filled MAC: same device, hostname filled.
		iface, ok, _ := st.FindIfaceByMAC(ctx, "aa:bb:cc:00:01:01")
		if !ok || iface.ID != nas || iface.Hostname == nil || *iface.Hostname != "nas" {
			t.Fatalf("nas iface = %+v", iface)
		}
		if n, detail := stats.Status(); n != 1 || detail != "kea: 1 leases, 0 static, 0 new, 1 MACs from ARP" {
			t.Fatalf("status = %d %q", n, detail)
		}
	})
}

// A key without the diagnostics privilege still syncs leases; the status says
// the ARP table was not read.
func TestSyncARPUnavailable(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := &fake{bodies: map[string]string{iscPath: iscJSON}, codes: map[string]int{arpPath: 403}}
	sync, _ := newSync(t, st, f)
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	stats, err := sync.RunOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 2 || !stats.ARPUnavailable {
		t.Fatalf("stats = %+v", stats)
	}
	if _, detail := stats.Status(); detail != "isc: 1 leases, 1 static, 2 new, ARP table unavailable" {
		t.Fatalf("detail = %q", detail)
	}
	rows, _ := st.ListDevices(t.Context())
	for _, r := range rows {
		if r.Source != "opnsense" {
			t.Fatalf("device %q source %q", r.Name, r.Source)
		}
	}
}
