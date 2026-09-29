package proxmox

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

// switchableServer answers the guest list with whatever resources holds (or a
// 500 when it holds ""), and every guest config with no NICs.
func switchableServer(t *testing.T, resources *atomic.Value) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		body := resources.Load().(string)
		if body == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(body))
	})
	mux.HandleFunc("/api2/json/nodes/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const twoGuests = `{"data":[
 {"vmid":100,"name":"web","node":"pve1","status":"running","type":"qemu"},
 {"vmid":101,"name":"db","node":"pve1","status":"running","type":"lxc"}]}`
const oneGuest = `{"data":[
 {"vmid":100,"name":"web","node":"pve1","status":"running","type":"qemu"}]}`

func eventsOfType(t *testing.T, st *store.Store, typ string) []store.Event {
	t.Helper()
	evs, err := st.ListEvents(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Event
	for _, e := range evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// A guest deleted in Proxmox is marked missing (never deleted) with one event,
// stays quiet while it stays gone, and is cleared with an event when it comes
// back. Failed and empty lists mark nothing.
func TestSyncFlagsGuestsMissingUpstream(t *testing.T) {
	storetest.EachDialect(t, testSyncFlagsGuestsMissingUpstream)
}

func testSyncFlagsGuestsMissingUpstream(t *testing.T, st *store.Store) {
	var res atomic.Value
	res.Store(twoGuests)
	srv := switchableServer(t, &res)
	sync := NewSync(st, NewClient(srv.URL, "root@pam!netis", "s3cret", false),
		events.NewService(st, events.NewBroker()))
	run := func(wantErr bool) {
		t.Helper()
		if _, err := sync.RunOnce(t.Context()); (err != nil) != wantErr {
			t.Fatalf("RunOnce err=%v, wantErr=%v", err, wantErr)
		}
	}
	missing := func(vmid int64) *string {
		t.Helper()
		id, ok, err := st.FindProxmoxGuest(t.Context(), vmid)
		if err != nil || !ok {
			t.Fatalf("guest %d: ok=%v err=%v", vmid, ok, err)
		}
		d, _ := st.GetDevice(t.Context(), id)
		return d.UpstreamMissingSince
	}

	run(false)
	if missing(100) != nil || missing(101) != nil {
		t.Fatal("guests marked missing while listed")
	}

	res.Store(oneGuest)
	run(false)
	run(false) // a second run must not raise a second event
	if missing(101) == nil {
		t.Fatal("deleted guest not marked missing")
	}
	if missing(100) != nil {
		t.Fatal("listed guest marked missing")
	}
	if evs := eventsOfType(t, st, "device_missing"); len(evs) != 1 || evs[0].Details != "Proxmox guest db is no longer in Proxmox" {
		t.Fatalf("device_missing events = %+v, want exactly one, in sentence case", evs)
	}
	if rows, _ := st.ListDevices(t.Context()); len(rows) != 3 {
		t.Fatalf("devices = %d, want node + 2 guests (nothing deleted)", len(rows))
	}

	// An API failure or an empty list says nothing about which guests exist.
	res.Store("")
	run(true)
	res.Store(`{"data":[]}`)
	run(false)
	if missing(100) != nil {
		t.Fatal("a failed or empty guest list marked a guest missing")
	}

	res.Store(twoGuests)
	run(false)
	if missing(101) != nil {
		t.Fatal("returned guest still marked missing")
	}
	if evs := eventsOfType(t, st, "device_returned"); len(evs) != 1 || evs[0].Details != "Proxmox guest db is back in Proxmox" {
		t.Fatalf("device_returned events = %+v, want exactly one, in sentence case", evs)
	}
}
