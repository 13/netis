package autofill

import (
	"context"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

// discovered creates a device the way a sweep does, in 10.0.0.0/24.
func discovered(t *testing.T, st *store.Store, name, mac, hostname, ip string) int64 {
	t.Helper()
	subs, err := st.ListSubnets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var sn int64
	if len(subs) > 0 {
		sn = subs[0].ID
	} else if sn, err = st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan"}); err != nil {
		t.Fatal(err)
	}
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	id, _, err := st.CreateDiscoveredDevice(t.Context(), store.Device{Name: name, Kind: "other", Source: "scan"},
		&mac, hp, sn, ip, "dhcp")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestServiceFillsAndRespectsPeople(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		svc := New(st)
		id := discovered(t, st, "BRW3C2AF4A1B2C3", "00:1b:a9:00:00:01", "BRW3C2AF4A1B2C3", "10.0.0.5")
		if err := svc.Run(ctx); err != nil {
			t.Fatal(err)
		}
		d, _ := st.GetDevice(ctx, id)
		if d.Kind != "printer" || d.Vendor != "Brother" || d.Reviewed {
			t.Fatalf("after first pass: %+v", d)
		}
		// Audit entry names netis and what changed.
		entries, _, _ := st.ListAudit(ctx, store.AuditFilter{Action: "device.autofill", Limit: 10})
		if len(entries) != 1 || entries[0].Username != "netis" || !strings.Contains(entries[0].Detail, "kind=printer") {
			t.Fatalf("audit = %+v", entries)
		}

		// A person changes the vendor; the next pass keeps it and the field
		// becomes theirs.
		d.Vendor = "Mine"
		if err := st.UpdateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
		if err := svc.Run(ctx, id); err != nil {
			t.Fatal(err)
		}
		d, _ = st.GetDevice(ctx, id)
		if d.Vendor != "Mine" {
			t.Fatalf("vendor clobbered: %+v", d)
		}
		recs, _ := st.ListAutofill(ctx, id)
		for _, r := range recs {
			if r.Field == "vendor" && r.State != store.AutofillOwned {
				t.Fatalf("vendor record = %+v", r)
			}
		}
	})
}

func TestServiceRemovedTagStaysRemoved(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		svc := New(st)
		id := discovered(t, st, "diskstation", "00:11:32:00:00:02", "diskstation", "10.0.0.6")
		svc.Run(ctx)
		tags, _ := st.DeviceTags(ctx, id)
		if len(tags) != 1 || tags[0].Name != "nas" {
			t.Fatalf("tags = %+v", tags)
		}
		if err := st.SetDeviceTags(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		svc.Run(ctx)
		svc.Run(ctx)
		if tags, _ := st.DeviceTags(ctx, id); len(tags) != 0 {
			t.Fatalf("removed tag came back: %+v", tags)
		}
	})
}

func TestServiceOffDoesNothing(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		st.SetSetting(ctx, "autofill_enabled", "off")
		id := discovered(t, st, "BRW3C2AF4A1B2C3", "00:1b:a9:00:00:01", "BRW3C2AF4A1B2C3", "10.0.0.5")
		New(st).Run(ctx)
		if d, _ := st.GetDevice(ctx, id); d.Kind != "other" {
			t.Fatalf("filled while off: %+v", d)
		}
	})
}

func TestKickCoalescesAndStartStops(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		svc := New(st)
		svc.interval = 10 * time.Millisecond
		passes := 0
		svc.afterPass = func() { passes++ }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		for i := 0; i < 5; i++ {
			svc.Kick()
		}
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
		// startup pass + at most two for the burst of kicks
		if passes < 2 || passes > 3 {
			t.Fatalf("passes = %d", passes)
		}
	})
}
