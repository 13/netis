package pihole

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const (
	leasesJSON = `{"leases":[
	  {"ip":"10.0.0.10","hwaddr":"AA:BB:CC:00:00:10","name":"laptop","expires":1893456000},
	  {"ip":"10.0.0.11","hwaddr":"AA:BB:CC:00:00:11","name":"","expires":0}
	]}`
	dhcpHostsJSON = `{"config":{"dhcp":{"hosts":["AA:BB:CC:00:00:20,10.0.0.20,printer","BADENTRY"]}}}`
	dnsHostsJSON  = `{"config":{"dns":{"hosts":["10.0.0.20 printer.lan","10.0.0.30 nas.lan"]}}}`
)

// fixtureServer serves /api/auth (returns a SID) and the three read endpoints.
// authHits counts /api/auth calls so tests can assert the re-auth behavior.
func fixtureServer(t *testing.T, authHits *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(authHits, 1)
		w.Write([]byte(`{"session":{"sid":"SID123","valid":true}}`))
	})
	guard := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-FTL-SID") != "SID123" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/dhcp/leases", guard(leasesJSON))
	mux.HandleFunc("/api/config/dhcp/hosts", guard(dhcpHostsJSON))
	mux.HandleFunc("/api/config/dns/hosts", guard(dnsHostsJSON))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLeases(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, &hits)
	c := NewClient(srv.URL, "pw", false)
	leases, err := c.Leases(context.Background())
	if err != nil || len(leases) != 2 {
		t.Fatalf("leases=%+v err=%v", leases, err)
	}
	if leases[0].IP != "10.0.0.10" || leases[0].MAC != "aa:bb:cc:00:00:10" ||
		leases[0].Hostname != "laptop" || leases[0].Expiry != 1893456000 {
		t.Fatalf("lease0=%+v", leases[0])
	}
	if hits != 1 {
		t.Fatalf("expected one auth, got %d", hits)
	}
}

func TestNormMACRejectsGarbage(t *testing.T) {
	if got := normMAC("AA:BB:CC:00:00:10"); got != "aa:bb:cc:00:00:10" {
		t.Errorf("valid MAC = %q", got)
	}
	// 17 chars but not hex pairs — must be rejected, not passed through.
	for _, bad := range []string{"zz:zz:zz:zz:zz:zz", "10.0.0.1", "aa:bb:cc:dd:ee", "not-a-mac-at-all!"} {
		if got := normMAC(bad); got != "" {
			t.Errorf("normMAC(%q) = %q, want empty", bad, got)
		}
	}
}

func TestReservationsSkipsMalformed(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, &hits)
	c := NewClient(srv.URL, "pw", false)
	res, err := c.Reservations(context.Background())
	if err != nil || len(res) != 1 {
		t.Fatalf("reservations=%+v err=%v", res, err)
	}
	if res[0].MAC != "aa:bb:cc:00:00:20" || res[0].IP != "10.0.0.20" || res[0].Hostname != "printer" {
		t.Fatalf("res0=%+v", res[0])
	}
}

func TestDNSRecords(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, &hits)
	c := NewClient(srv.URL, "pw", false)
	recs, err := c.DNSRecords(context.Background())
	if err != nil || len(recs) != 2 {
		t.Fatalf("records=%+v err=%v", recs, err)
	}
	if recs[0].IP != "10.0.0.20" || recs[0].Name != "printer.lan" {
		t.Fatalf("rec0=%+v", recs[0])
	}
}

func TestReauthOn401(t *testing.T) {
	// Server rejects the first SID once, forcing a single re-auth + retry.
	var hits int32
	var rejectNext atomic.Bool
	rejectNext.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"session":{"sid":"SID123","valid":true}}`))
	})
	mux.HandleFunc("/api/dhcp/leases", func(w http.ResponseWriter, r *http.Request) {
		if rejectNext.Swap(false) {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(leasesJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "pw", false)
	leases, err := c.Leases(context.Background())
	if err != nil || len(leases) != 2 {
		t.Fatalf("leases=%+v err=%v", leases, err)
	}
	if hits != 2 {
		t.Fatalf("expected re-auth (2 auth calls), got %d", hits)
	}
}
