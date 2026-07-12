package web

import (
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestGridStates(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})

	mk := func(name, ip string) int64 {
		devID, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		ifID, _ := st.AddIface(devID, nil, nil)
		st.AssignIP(ifID, snID, ip, "static")
		return ifID
	}
	onlineIf := mk("gw", "10.0.0.1")
	st.MarkSeen(onlineIf, 1, time.Now())
	offlineIf := mk("nas", "10.0.0.2")
	st.MarkSeen(offlineIf, 1, time.Now())
	st.MarkMissed(offlineIf, 1)
	mk("printer", "10.0.0.3") // reserved: assigned, never seen
	// conflict: second iface claims .1
	dup, _ := st.CreateDevice(store.Device{Name: "ghost", Kind: "other", Source: "manual"})
	dupIf, _ := st.AddIface(dup, nil, nil)
	st.AssignIP(dupIf, snID, "10.0.0.1", "static")

	rec := authedGet(t, srv, st, "/subnets/1/grid")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"sq conflict", "sq offline", "sq reserved", "sq free", "sq edge", "static"} {
		if !strings.Contains(body, want) {
			t.Errorf("grid missing %q", want)
		}
	}
	// /29 full range → 8 squares (network + 6 hosts + broadcast)
	if n := strings.Count(body, `class="sq`); n != 8 {
		t.Errorf("squares=%d, want 8", n)
	}
}
