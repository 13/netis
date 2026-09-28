package scan

import (
	"context"
	"strings"
	"testing"

	"netis/internal/store"
)

func conflictEvents(t *testing.T, st *store.Store) []store.Event {
	t.Helper()
	evs, err := st.ListEvents(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Event
	for _, e := range evs {
		if e.Type == "ip_conflict" {
			out = append(out, e)
		}
	}
	return out
}

// A sweep announces an IP the moment a second interface claims it, and not
// again while the conflict lasts; once it clears, a new conflict is news.
func TestSweepAnnouncesNewIPConflictsOnce(t *testing.T) {
	e, st, _, snID := testEngine(t)
	ctx := context.Background()
	sn, _ := st.GetSubnet(ctx, snID)
	claim := func(name string) int64 {
		d, _ := st.CreateDevice(ctx, store.Device{Name: name, Kind: "other", Source: "manual"})
		f, _ := st.AddIface(ctx, d, nil, nil)
		if _, err := st.AssignIP(ctx, f, snID, "10.0.0.20", "dhcp"); err != nil {
			t.Fatal(err)
		}
		return f
	}
	claim("printer")
	if err := e.RunSubnet(ctx, sn); err != nil {
		t.Fatal(err)
	}
	if n := len(conflictEvents(t, st)); n != 0 {
		t.Fatalf("%d conflict events with one claimant", n)
	}

	second := claim("camera")
	e.RunSubnet(ctx, sn)
	evs := conflictEvents(t, st)
	if len(evs) != 1 || !strings.Contains(evs[0].Details, "10.0.0.20") ||
		!strings.Contains(evs[0].Details, "camera, printer") {
		t.Fatalf("after second claimant: %+v", evs)
	}

	e.RunSubnet(ctx, sn)
	if n := len(conflictEvents(t, st)); n != 1 {
		t.Fatalf("conflict re-announced on the next sweep: %d events", n)
	}

	// Cleared, then back: announced again.
	st.RemoveIfaceIPsInSubnetExcept(ctx, second, snID, "")
	e.RunSubnet(ctx, sn)
	st.AssignIP(ctx, second, snID, "10.0.0.20", "dhcp")
	e.RunSubnet(ctx, sn)
	if n := len(conflictEvents(t, st)); n != 2 {
		t.Fatalf("returning conflict: %d events, want 2", n)
	}
}
