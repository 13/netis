package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"netis/internal/store"
)

// adminToken sets up an onboarded server with an admin holding an API token.
func adminToken(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	id := addUser(t, st, "ben", "password1", "admin", "bensess")
	tokenFor(t, st, id, "netis_admin", "")
	return srv, st, "netis_admin"
}

func decodeDevice(t *testing.T, body []byte) apiDevice {
	t.Helper()
	var d apiDevice
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return d
}

func TestAPICreateDevice(t *testing.T) {
	srv, st, tok := adminToken(t)
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/16", Name: "wide", Kind: "lan", ScanIntervalSec: 120})

	rec := bearer(t, srv, "POST", "/api/devices", tok,
		`{"name":" nas ","kind":"server","mac":"AA-BB-CC-00-00-01","ip":"10.0.0.9","tags":["core","lab"],"notes":"n"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: code=%d body=%s", rec.Code, rec.Body.String())
	}
	d := decodeDevice(t, rec.Body.Bytes())
	if rec.Header().Get("Location") != "/api/devices/"+itoa(d.ID) {
		t.Errorf("Location=%q", rec.Header().Get("Location"))
	}
	if d.Name != "nas" || d.Kind != "server" || d.Source != "manual" || d.Notes != "n" ||
		!reflect.DeepEqual(d.MACs, []string{"aa:bb:cc:00:00:01"}) ||
		!reflect.DeepEqual(d.Tags, []string{"core", "lab"}) ||
		len(d.IPs) != 1 || d.IPs[0].IP != "10.0.0.9" || d.IPs[0].Kind != "static" {
		t.Fatalf("created %+v", d)
	}
	// The narrowest subnet holding the address was picked.
	if ifc, ok, _ := st.FindIfaceByIP(t.Context(), snID, "10.0.0.9"); !ok || ifc.DeviceID != d.ID {
		t.Errorf("IP not assigned in the /24")
	}

	// Minimal create: name and kind only.
	if rec := bearer(t, srv, "POST", "/api/devices", tok, `{"name":"bare","kind":"other"}`); rec.Code != http.StatusCreated {
		t.Fatalf("bare create: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPICreateDeviceErrors(t *testing.T) {
	srv, st, tok := adminToken(t)
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	if rec := bearer(t, srv, "POST", "/api/devices", tok, `{"name":"a","kind":"other","mac":"aa:bb:cc:00:00:01"}`); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	before := deviceCount(t, st)
	for _, c := range []struct {
		name, body string
		code       int
		msg        string
	}{
		{"no name", `{"kind":"other"}`, 400, "name required"},
		{"bad kind", `{"name":"x","kind":"toaster"}`, 400, "bad kind"},
		{"bad mac", `{"name":"x","kind":"other","mac":"zz"}`, 400, "invalid MAC"},
		{"bad ip", `{"name":"x","kind":"other","ip":"10.0.0.999"}`, 400, "invalid IP"},
		{"ip in no subnet", `{"name":"x","kind":"other","ip":"192.168.9.9"}`, 400, "no configured subnet"},
		{"ip outside named subnet", `{"name":"x","kind":"other","ip":"192.168.9.9","subnet_id":` + itoa(snID) + `}`, 400, "not in subnet"},
		{"unknown subnet", `{"name":"x","kind":"other","ip":"10.0.0.5","subnet_id":999}`, 400, "unknown subnet"},
		{"unknown parent", `{"name":"x","kind":"other","parent_device_id":999}`, 400, "parent device does not exist"},
		{"duplicate mac", `{"name":"x","kind":"other","mac":"AA:BB:CC:00:00:01"}`, 409, "already belongs"},
		{"unknown field", `{"name":"x","kind":"other","colour":"red"}`, 400, "unknown field"},
		{"not json", `{"name":`, 400, "malformed JSON"},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantJSONError(t, bearer(t, srv, "POST", "/api/devices", tok, c.body), c.code, c.msg)
		})
	}
	// A form-encoded body is refused rather than read as empty JSON.
	req := bearer(t, srv, "POST", "/api/devices", tok, "")
	wantJSONError(t, req, http.StatusUnsupportedMediaType, "application/json")
	if n := deviceCount(t, st); n != before {
		t.Errorf("refused creates left devices: %d -> %d", before, n)
	}
}

func TestAPIPatchDevice(t *testing.T) {
	srv, st, tok := adminToken(t)
	parent, _ := st.CreateDevice(t.Context(), store.Device{Name: "host", Kind: "server", Source: "manual"})
	id, _ := st.CreateDevice(t.Context(), store.Device{Name: "guest", Kind: "vm", Source: "scan", Notes: "keep", ParentDeviceID: &parent})
	st.SetDeviceTags(t.Context(), id, []string{"old"})

	rec := bearer(t, srv, "PATCH", "/api/devices/"+itoa(id), tok, `{"name":"web","tags":["a","b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	d := decodeDevice(t, rec.Body.Bytes())
	if d.Name != "web" || d.Kind != "vm" || d.Notes != "keep" || d.ParentID == nil || *d.ParentID != parent ||
		!reflect.DeepEqual(d.Tags, []string{"a", "b"}) || !d.Reviewed {
		t.Fatalf("after patch %+v", d)
	}

	// Leaving parent_device_id out keeps it; null clears it. Tags left out stay.
	rec = bearer(t, srv, "PATCH", "/api/devices/"+itoa(id), tok, `{"parent_device_id":null,"notes":""}`)
	d = decodeDevice(t, rec.Body.Bytes())
	if d.ParentID != nil || d.Notes != "" || len(d.Tags) != 2 {
		t.Fatalf("after clearing %+v", d)
	}

	for _, c := range []struct {
		name, path, body string
		code             int
		msg              string
	}{
		{"missing device", "/api/devices/999", `{"name":"x"}`, 404, "not found"},
		{"non-numeric id", "/api/devices/abc", `{"name":"x"}`, 404, "not found"},
		{"bad kind", "/api/devices/" + itoa(id), `{"kind":"toaster"}`, 400, "bad kind"},
		{"blank name", "/api/devices/" + itoa(id), `{"name":"  "}`, 400, "name required"},
		{"null name", "/api/devices/" + itoa(id), `{"name":null}`, 400, "must be a string"},
		{"own parent", "/api/devices/" + itoa(id), `{"parent_device_id":` + itoa(id) + `}`, 400, "own parent"},
		{"unknown parent", "/api/devices/" + itoa(id), `{"parent_device_id":999}`, 400, "parent device does not exist"},
		{"read-only field", "/api/devices/" + itoa(id), `{"source":"proxmox"}`, 400, "read-only"},
		{"bad tags", "/api/devices/" + itoa(id), `{"tags":"a,b"}`, 400, "tags"},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantJSONError(t, bearer(t, srv, "PATCH", c.path, tok, c.body), c.code, c.msg)
		})
	}
	got, _ := st.GetDevice(t.Context(), id)
	if got.Name != "web" || got.Kind != "vm" || got.Source != "scan" {
		t.Errorf("a refused patch changed the device: %+v", got)
	}
}

func TestAPIDeleteDevice(t *testing.T) {
	srv, st, tok := adminToken(t)
	id, _ := st.CreateDevice(t.Context(), store.Device{Name: "gone", Kind: "other", Source: "manual"})
	if rec := bearer(t, srv, "DELETE", "/api/devices/"+itoa(id), tok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetDevice(t.Context(), id); err == nil {
		t.Fatal("device still there")
	}
	wantJSONError(t, bearer(t, srv, "DELETE", "/api/devices/"+itoa(id), tok, ""), 404, "not found")
}

// The write API works with the browser session too, as the read API does, and
// a viewer's session is refused with a JSON 403.
func TestAPIWritesWithSession(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addUser(t, st, "ben", "password1", "admin", "bensess")
	addUser(t, st, "eve", "password1", "viewer", "evesess")
	send := func(session string) *http.Response {
		req := httptest.NewRequest("POST", "/api/devices", strings.NewReader(`{"name":"s","kind":"other"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: session})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Result()
	}
	if res := send("bensess"); res.StatusCode != http.StatusCreated {
		t.Errorf("admin session create: %d", res.StatusCode)
	}
	if res := send("evesess"); res.StatusCode != http.StatusForbidden ||
		!strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("viewer session create: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}
