package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

// seedInventory creates one online device with an IP, a MAC and a tag, plus the
// subnet it lives in, and returns the device and subnet ids.
func seedInventory(t *testing.T, st *store.Store) (int64, int64) {
	t.Helper()
	snID, err := st.CreateSubnet(t.Context(), store.Subnet{
		CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	devID, err := st.CreateDevice(t.Context(), store.Device{
		Name: "gw", Kind: "router", Source: "manual", Vendor: "MikroTik",
	})
	if err != nil {
		t.Fatal(err)
	}
	mac := "bc:24:11:00:00:01"
	ifID, err := st.AddIface(t.Context(), devID, &mac, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIP(t.Context(), ifID, snID, "10.0.0.1", "static"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkSeen(t.Context(), ifID, 1.5, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDeviceTags(t.Context(), devID, []string{"core"}); err != nil {
		t.Fatal(err)
	}
	return devID, snID
}

func getJSON(t *testing.T, srv *Server, st *store.Store, path string, into any) *httptest.ResponseRecorder {
	t.Helper()
	rec := authedGet(t, srv, st, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: code=%d body=%s", path, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET %s: Content-Type=%q", path, ct)
	}
	if into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("GET %s: %v body=%s", path, err, rec.Body.String())
		}
	}
	return rec
}

func TestAPIDevices(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := seedInventory(t, st)

	var list struct{ Devices []apiDevice }
	getJSON(t, srv, st, "/api/devices", &list)
	if len(list.Devices) != 1 {
		t.Fatalf("devices=%+v", list.Devices)
	}
	d := list.Devices[0]
	if d.ID != devID || d.Name != "gw" || !d.Online {
		t.Fatalf("device=%+v", d)
	}
	if len(d.IPs) != 1 || d.IPs[0].IP != "10.0.0.1" || d.IPs[0].Kind != "static" {
		t.Errorf("ips=%+v", d.IPs)
	}
	if len(d.MACs) != 1 || len(d.Tags) != 1 || d.Tags[0] != "core" {
		t.Errorf("macs=%v tags=%v", d.MACs, d.Tags)
	}

	var one apiDevice
	getJSON(t, srv, st, "/api/devices/"+itoa(devID), &one)
	if one.ID != devID {
		t.Fatalf("device=%+v", one)
	}
	if rec := authedGet(t, srv, st, "/api/devices/9999"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown device: code=%d", rec.Code)
	}
}

// A device with nothing attached must still marshal its list fields as arrays:
// a caller iterating them should not have to handle null.
func TestAPIDeviceEmptyListsAreArrays(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	if _, err := st.CreateDevice(t.Context(), store.Device{Name: "bare", Kind: "other", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	body := getJSON(t, srv, st, "/api/devices", nil).Body.String()
	for _, want := range []string{`"ips":[]`, `"macs":[]`, `"tags":[]`} {
		if !strings.Contains(strings.ReplaceAll(body, " ", ""), want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}

func TestAPISubnetsAndEvents(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	_, snID := seedInventory(t, st)
	for i := 0; i < 3; i++ {
		if _, err := st.AddEvent(t.Context(), "device_new", nil, "seeded"); err != nil {
			t.Fatal(err)
		}
	}

	var subnets struct{ Subnets []apiSubnet }
	getJSON(t, srv, st, "/api/subnets", &subnets)
	if len(subnets.Subnets) != 1 || subnets.Subnets[0].ID != snID || subnets.Subnets[0].CIDR != "10.0.0.0/24" {
		t.Fatalf("subnets=%+v", subnets.Subnets)
	}

	var evs struct{ Events []apiEvent }
	getJSON(t, srv, st, "/api/events", &evs)
	if len(evs.Events) != 3 {
		t.Fatalf("events=%d", len(evs.Events))
	}
	var limited struct{ Events []apiEvent }
	getJSON(t, srv, st, "/api/events?limit=2", &limited)
	if len(limited.Events) != 2 {
		t.Fatalf("limited events=%d", len(limited.Events))
	}
	if rec := authedGet(t, srv, st, "/api/events?limit=nonsense"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: code=%d", rec.Code)
	}
	// An outsized limit is clamped, not refused: asking for everything is a
	// reasonable thing to do, marshalling everything is not.
	getJSON(t, srv, st, "/api/events?limit=999999", nil)
}

func TestAPIStatus(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	seedInventory(t, st)
	var status apiStatus
	getJSON(t, srv, st, "/api/status", &status)
	if status.Devices != 1 || status.DevicesOnline != 1 || status.Subnets != 1 {
		t.Fatalf("status=%+v", status)
	}
	if status.Backend != "sqlite" || status.Version == "" {
		t.Fatalf("status=%+v", status)
	}
}

// The API answers with a status code, not the login page a browser would get:
// a script following a 303 to /login would parse HTML as its data.
func TestAPIRequiresAuthWithoutRedirecting(t *testing.T) {
	srv, _ := testServer(t)
	for _, path := range []string{"/api/devices", "/api/subnets", "/api/events", "/api/status", "/metrics"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code=%d, want 401", path, rec.Code)
		}
	}
}

// Onboarding is a browser flow; a data request must not be answered with a
// redirect into the wizard.
func TestAPIIsReachableBeforeOnboarding(t *testing.T) {
	srv, st := testServer(t)
	// onboarded deliberately unset
	seedInventory(t, st)
	var list struct{ Devices []apiDevice }
	getJSON(t, srv, st, "/api/devices", &list)
	if len(list.Devices) != 1 {
		t.Fatalf("devices=%+v", list.Devices)
	}
}

func TestMetricsWithSession(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	seedInventory(t, st)
	if err := st.SetIntegrationStatus(t.Context(), store.IntegrationStatus{
		Name: "scan", LastRun: time.Now().UTC().Format(time.RFC3339), OK: true, Detail: "scanned 10.0.0.0/24",
	}); err != nil {
		t.Fatal(err)
	}
	rec := authedGet(t, srv, st, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE netis_devices gauge",
		"netis_devices 1",
		"netis_devices_online 1",
		"netis_subnets 1",
		`netis_subnet_scan_enabled{cidr="10.0.0.0/24",name="lab",kind="lan"} 1`,
		`netis_integration_last_ok{name="scan"} 1`,
		"netis_build_info{version=",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n%s", want, body)
		}
	}
}

func TestMetricsScrapeToken(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := NewServer(st, events.NewBroker(), nil, nil, Options{MetricsToken: "scrape-me"})
	addAdmin(t, st)

	get := func(auth string) int {
		req := httptest.NewRequest("GET", "/metrics", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := get("Bearer scrape-me"); code != http.StatusOK {
		t.Errorf("valid token: code=%d, want 200", code)
	}
	if code := get("bearer scrape-me"); code != http.StatusOK {
		t.Errorf("scheme should be case-insensitive: code=%d", code)
	}
	if code := get("Bearer wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong token: code=%d, want 401", code)
	}
	if code := get(""); code != http.StatusUnauthorized {
		t.Errorf("no token: code=%d, want 401", code)
	}
	// The token is for /metrics alone — it is a scrape credential, not a login.
	req := httptest.NewRequest("GET", "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer scrape-me")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token on /api/devices: code=%d, want 401", rec.Code)
	}
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
