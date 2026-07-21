package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type recordingTrigger struct {
	mu  sync.Mutex
	ids []int64
}

func (r *recordingTrigger) Trigger(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
}

func (r *recordingTrigger) got() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.ids))
	copy(out, r.ids)
	return out
}

// testServerTrig builds a server wired to a recording ScanTrigger.
func testServerTrig(t *testing.T) (*Server, *store.Store, *recordingTrigger) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	trig := &recordingTrigger{}
	return NewServer(st, events.NewBroker(), trig, nil), st, trig
}

func TestScanNowLanTriggersAndToasts(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: false, ScanIntervalSec: 120})
	rec := authedPost(t, srv, st, "/subnets/1/scan", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("scan now code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="toast"`) || !strings.Contains(body, "10.0.0.0/24") {
		t.Fatalf("scan-now body missing toast/CIDR: %s", body)
	}
	if ids := trig.got(); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("trigger ids=%v, want [1]", ids)
	}
}

func TestScanNowWireGuardDoesNotTrigger(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.9.0.0/24", Name: "wg", Kind: "wireguard", ScanIntervalSec: 120})
	rec := authedPost(t, srv, st, "/subnets/1/scan", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not scannable") {
		t.Fatalf("expected not-scannable toast: %s", rec.Body.String())
	}
	if ids := trig.got(); len(ids) != 0 {
		t.Fatalf("wireguard must not trigger, ids=%v", ids)
	}
}

func TestScanNowNonexistent404(t *testing.T) {
	srv, st, _ := testServerTrig(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedPost(t, srv, st, "/subnets/999/scan", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d, want 404", rec.Code)
	}
}

func TestScanAllTriggersNonWireGuard(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.9.0.0/24", Name: "wg", Kind: "wireguard", ScanIntervalSec: 120})
	rec := authedPost(t, srv, st, "/scan", url.Values{})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "all subnets") {
		t.Fatalf("scan-all code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ids := trig.got(); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("scan-all should trigger only the LAN subnet, ids=%v", ids)
	}
}

func TestScanButtonsRendered(t *testing.T) {
	srv, st, _ := testServerTrig(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	dash := authedGet(t, srv, st, "/").Body.String()
	if !strings.Contains(dash, `hx-post="/scan"`) {
		t.Error("dashboard missing Scan all button")
	}
	if !strings.Contains(dash, `hx-post="/subnets/1/scan"`) {
		t.Error("dashboard subnet card missing Scan button")
	}

	devs := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(devs, `hx-post="/scan"`) {
		t.Error("device list toolbar missing Scan all button")
	}

	settings := authedGet(t, srv, st, "/settings").Body.String()
	if !strings.Contains(settings, `hx-post="/subnets/1/scan"`) {
		t.Error("settings row missing scan button")
	}
	if !strings.Contains(settings, "Auto-scan") {
		t.Error("settings should relabel Scan enabled -> Auto-scan")
	}
}

func TestScanRoutesRequireAdmin(t *testing.T) {
	srv, st, _ := testServerTrig(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	uID, _ := st.CreateUser(t.Context(), "eve", "h", "viewer")
	st.CreateSession(t.Context(), "viewertok", uID, "2099-01-01T00:00:00Z")
	for _, path := range []string{"/subnets/1/scan", "/scan"} {
		req := httptest.NewRequest("POST", path, nil)
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer POST %s = %d, want 403", path, rec.Code)
		}
	}
}
