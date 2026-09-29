package store_test

import (
	"testing"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

func TestHintsReplacePerSource(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		id, err := st.CreateDevice(ctx, store.Device{Name: "d", Kind: "other", Source: "scan"})
		if err != nil {
			t.Fatal(err)
		}
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(st.ReplaceHints(ctx, id, "oui", []store.Hint{{Field: "vendor", Value: "Apple", Confidence: 90, Detail: "MAC 3c:07:54:00:00:01", SeenAt: "2026-09-29T00:00:00Z"}}))
		must(st.ReplaceHints(ctx, id, "hostname", []store.Hint{{Field: "kind", Value: "phone", Confidence: 70, SeenAt: "2026-09-29T00:00:00Z"}}))
		must(st.ReplaceHints(ctx, id, "oui", nil)) // oui has nothing now
		hs, err := st.ListHints(ctx, id)
		must(err)
		if len(hs) != 1 || hs[0].Source != "hostname" || hs[0].Value != "phone" || hs[0].DeviceID != id {
			t.Fatalf("hints = %+v", hs)
		}
		must(st.DeleteDevice(ctx, id))
		hs, _ = st.ListHints(ctx, id)
		if len(hs) != 0 {
			t.Fatalf("hints survived device delete: %+v", hs)
		}
	})
}

func TestApplyAutofillCompareAndSwap(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		id, _ := st.CreateDevice(ctx, store.Device{Name: "unknown-aa", Kind: "other", Source: "scan"})
		writes, tags, err := st.ApplyAutofill(ctx, id, store.AutofillChanges{
			Writes: []store.AutofillWrite{
				{Field: "vendor", Value: "Brother", Source: "hostname", Expect: ""},
				{Field: "kind", Value: "printer", Source: "hostname", Expect: "other"},
				// Stale read: model is "" but we claim "X", so this write is skipped.
				{Field: "model", Value: "HL-L2350", Source: "hostname", Expect: "X"},
			},
			Tags: []store.AutofillTag{{Name: "office", Source: "hostname"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != 2 || len(tags) != 1 {
			t.Fatalf("applied writes=%+v tags=%+v", writes, tags)
		}
		d, _ := st.GetDevice(ctx, id)
		if d.Vendor != "Brother" || d.Kind != "printer" || d.Model != "" || d.Reviewed {
			t.Fatalf("device = %+v", d)
		}
		recs, _ := st.ListAutofill(ctx, id)
		got := map[string]store.AutofillRecord{}
		for _, r := range recs {
			got[r.Field] = r
		}
		if got["vendor"].Value != "Brother" || got["vendor"].State != store.AutofillApplied ||
			got["tag:office"].State != store.AutofillApplied || len(got) != 3 {
			t.Fatalf("records = %+v", recs)
		}

		// A person owns vendor now; Own flips the record.
		if _, _, err := st.ApplyAutofill(ctx, id, store.AutofillChanges{Own: []string{"vendor"}}); err != nil {
			t.Fatal(err)
		}
		recs, _ = st.ListAutofill(ctx, id)
		for _, r := range recs {
			if r.Field == "vendor" && r.State != store.AutofillOwned {
				t.Fatalf("vendor record = %+v", r)
			}
		}

		// Unreviewed guard: once reviewed, an Unreviewed write is skipped.
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(st.SetDeviceReviewed(ctx, id, true))
		writes, _, err = st.ApplyAutofill(ctx, id, store.AutofillChanges{Writes: []store.AutofillWrite{
			{Field: "name", Value: "Printer", Source: "mdns", Expect: "unknown-aa", Unreviewed: true},
		}})
		must(err)
		if len(writes) != 0 {
			t.Fatalf("wrote to a reviewed device: %+v", writes)
		}

		// An unknown field is an error, not SQL injection.
		if _, _, err := st.ApplyAutofill(ctx, id, store.AutofillChanges{Writes: []store.AutofillWrite{{Field: "reviewed", Value: "1"}}}); err == nil {
			t.Fatal("want error for unknown field")
		}
	})
}

func TestDeviceIDs(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		a, _ := st.CreateDevice(t.Context(), store.Device{Name: "a", Kind: "other", Source: "manual"})
		b, _ := st.CreateDevice(t.Context(), store.Device{Name: "b", Kind: "other", Source: "manual"})
		ids, err := st.DeviceIDs(t.Context())
		if err != nil || len(ids) != 2 || ids[0] != a || ids[1] != b {
			t.Fatalf("ids=%v err=%v", ids, err)
		}
	})
}
