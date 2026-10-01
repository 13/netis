package adguard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netis/internal/events"
	"netis/internal/leases"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

const statusJSON = `{
  "enabled": %s,
  "interface_name": "eth0",
  "v4": {"gateway_ip": "10.0.0.1", "range_start": "10.0.0.100", "range_end": "10.0.0.200"},
  "leases": [
    {"mac": "AA:BB:CC:00:00:10", "ip": "10.0.0.110", "hostname": "laptop", "expires": "2026-09-29T10:00:00Z"},
    {"mac": "not-a-mac", "ip": "10.0.0.111", "hostname": "junk", "expires": "2026-09-29T10:00:00Z"}
  ],
  "static_leases": [
    {"mac": "aa:bb:cc:00:00:20", "ip": "10.0.0.20", "hostname": "printer"}
  ]
}`

// fakeAdGuard serves /control/dhcp/status under prefix, requiring basic auth
// admin/pw.
func fakeAdGuard(t *testing.T, prefix string, enabled bool) *httptest.Server {
	t.Helper()
	body := strings.Replace(statusJSON, "%s", map[bool]string{true: "true", false: "false"}[enabled], 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+prefix+"/control/dhcp/status", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDHCPStatus(t *testing.T) {
	// A base URL with a path prefix and a trailing slash, as behind a proxy.
	srv := fakeAdGuard(t, "/adguard", true)
	c := NewClient(srv.URL+"/adguard/", "admin", "pw", false)
	defer c.Close()
	d, err := c.DHCPStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Enabled || len(d.Leases) != 1 || len(d.Static) != 1 {
		t.Fatalf("status = %+v", d)
	}
	if l := d.Leases[0]; l.MAC != "aa:bb:cc:00:00:10" || l.IP != "10.0.0.110" || l.Hostname != "laptop" {
		t.Fatalf("lease = %+v", l)
	}
	if s := d.Static[0]; s.MAC != "aa:bb:cc:00:00:20" || s.IP != "10.0.0.20" || s.Hostname != "printer" {
		t.Fatalf("static = %+v", s)
	}
	if len(d.Pools) != 1 || d.Pools[0] != (leases.Range{Start: "10.0.0.100", End: "10.0.0.200"}) {
		t.Fatalf("pools = %+v", d.Pools)
	}
}

// A wrong password surfaces as an HTTP 401 error, which the runner files
// under "authentication failed".
func TestDHCPStatusBadAuth(t *testing.T) {
	srv := fakeAdGuard(t, "", true)
	c := NewClient(srv.URL, "admin", "wrong", false)
	_, err := c.DHCPStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v, want HTTP 401", err)
	}
}

func TestSyncAppliesLeases(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		snID, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
		if err != nil {
			t.Fatal(err)
		}
		srv := fakeAdGuard(t, "", true)
		c := NewClient(srv.URL, "admin", "pw", false)
		stats, err := NewSync(st, c, events.NewService(st, events.NewBroker())).RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Leases != 1 || stats.Static != 1 || stats.Created != 2 {
			t.Fatalf("stats = %+v", stats)
		}
		if n, detail := stats.Status(); n != 1 || detail != "1 leases, 1 static, 2 new" {
			t.Fatalf("status = %d %q", n, detail)
		}
		rows, _ := st.ListDevices(ctx)
		for _, r := range rows {
			if r.Source != "adguard" {
				t.Fatalf("device %q source %q, want adguard", r.Name, r.Source)
			}
		}
		iface, _, _ := st.FindIfaceByMAC(ctx, "aa:bb:cc:00:00:20")
		ips, _ := st.ListIPs(ctx, iface.ID)
		if len(ips) != 1 || ips[0].Kind != "static" {
			t.Fatalf("static lease ips = %+v", ips)
		}
		sn, err := st.GetSubnet(ctx, snID)
		if err != nil || sn.DHCPStart != "10.0.0.100" || sn.DHCPEnd != "10.0.0.200" || sn.DHCPPoolSource != "adguard" {
			t.Fatalf("subnet pool = %+v err=%v", sn, err)
		}
	})
}

// With AdGuard's DHCP server off the run succeeds, says so, and leaves the
// inventory alone even though the response still lists leases.
func TestSyncDHCPDisabled(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	srv := fakeAdGuard(t, "", false)
	stats, err := NewSync(st, NewClient(srv.URL, "admin", "pw", false), events.NewService(st, events.NewBroker())).RunOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n, detail := stats.Status(); n != 0 || detail != "DHCP server disabled in AdGuard Home" {
		t.Fatalf("status = %d %q", n, detail)
	}
	if rows, _ := st.ListDevices(t.Context()); len(rows) != 0 {
		t.Fatalf("disabled DHCP created devices: %+v", rows)
	}
}
