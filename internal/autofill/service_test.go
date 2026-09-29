package autofill

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
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
		var passes atomic.Int32
		svc.afterPass = func() { passes.Add(1) }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		for i := 0; i < 5; i++ {
			svc.Kick()
		}
		// Poll instead of a fixed sleep: wait for the startup pass plus the
		// coalesced burst pass, with a generous deadline for loaded CI.
		deadline := time.Now().Add(5 * time.Second)
		for passes.Load() < 2 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		// Let things settle a bit longer so a spurious extra pass would show up.
		time.Sleep(50 * time.Millisecond)
		cancel()
		<-done
		// startup pass + one for the burst of kicks, coalesced into one.
		if got := passes.Load(); got != 2 {
			t.Fatalf("passes = %d", got)
		}
	})
}

// TestSecondPassSkipsUnchangedHints guards against a pass rewriting hints
// (and their seen_at) when the source's content has not changed, which would
// cost 3 write transactions per device per pass for nothing and compete with
// sweeps on SQLite.
func TestSecondPassSkipsUnchangedHints(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		svc := New(st)
		id := discovered(t, st, "BRW3C2AF4A1B2C3", "00:1b:a9:00:00:01", "BRW3C2AF4A1B2C3", "10.0.0.5")

		first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return first }
		if err := svc.Run(ctx, id); err != nil {
			t.Fatal(err)
		}
		hints, err := st.ListHints(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(hints) == 0 {
			t.Fatal("no hints after first pass")
		}
		seenAt := map[string]string{}
		for _, h := range hints {
			seenAt[h.Source+"/"+h.Field+"/"+h.Value] = h.SeenAt
		}

		second := first.Add(time.Hour)
		svc.now = func() time.Time { return second }
		if err := svc.Run(ctx, id); err != nil {
			t.Fatal(err)
		}
		hints2, err := st.ListHints(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(hints2) != len(hints) {
			t.Fatalf("hint count changed across an unchanged pass: %d -> %d", len(hints), len(hints2))
		}
		for _, h := range hints2 {
			want, ok := seenAt[h.Source+"/"+h.Field+"/"+h.Value]
			if !ok {
				t.Fatalf("hint appeared that was not there before: %+v", h)
			}
			if h.SeenAt != want {
				t.Fatalf("seen_at moved for %s/%s/%s though nothing changed: %s -> %s", h.Source, h.Field, h.Value, want, h.SeenAt)
			}
		}
	})
}

// TestRunSerialisesConcurrentPasses guards against the same device being
// autofilled by two overlapping Run calls at once (e.g. Start's background
// pass and a direct Run from the web port-scan handler), which could
// otherwise interleave ReplaceHints calls into a primary-key conflict on
// Postgres.
func TestRunSerialisesConcurrentPasses(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		svc := New(st)
		id := discovered(t, st, "BRW3C2AF4A1B2C3", "00:1b:a9:00:00:01", "BRW3C2AF4A1B2C3", "10.0.0.5")

		var wg sync.WaitGroup
		errs := make(chan error, 10)
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 5; i++ {
					if err := svc.Run(ctx, id); err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}

		entries, _, err := st.ListAudit(ctx, store.AuditFilter{Action: "device.autofill", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("audit entries = %+v", entries)
		}
	})
}
