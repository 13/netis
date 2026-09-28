package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestDeviceAlertFlag(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, err := s.CreateDevice(t.Context(), Device{Name: "nas", Kind: "server", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		name, alert, err := s.DeviceAlert(t.Context(), id)
		if err != nil || name != "nas" || alert {
			t.Fatalf("new device: name=%q alert=%v err=%v, want nas/false", name, alert, err)
		}
		if err := s.SetDeviceAlertOffline(t.Context(), id, true); err != nil {
			t.Fatal(err)
		}
		// An edit through the form must not reset the flag.
		d, _ := s.GetDevice(t.Context(), id)
		d.Notes = "edited"
		if err := s.UpdateDevice(t.Context(), d); err != nil {
			t.Fatal(err)
		}
		if _, alert, _ := s.DeviceAlert(t.Context(), id); !alert {
			t.Fatal("flag lost after UpdateDevice")
		}
		if err := s.SetDeviceAlertOffline(t.Context(), id, false); err != nil {
			t.Fatal(err)
		}
		if _, alert, _ := s.DeviceAlert(t.Context(), id); alert {
			t.Fatal("flag still set after clearing")
		}
	})
}

func TestConflictingIPs(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		sn, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 60})
		if err != nil {
			t.Fatal(err)
		}
		iface := func(name string) int64 {
			d, err := s.CreateDevice(ctx, Device{Name: name, Kind: "other", Source: "manual"})
			if err != nil {
				t.Fatal(err)
			}
			f, err := s.AddIface(ctx, d, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
		a, b, c := iface("alpha"), iface("bravo"), iface("charlie")
		for _, x := range []struct {
			f  int64
			ip string
		}{{a, "10.0.0.5"}, {b, "10.0.0.5"}, {c, "10.0.0.6"}} {
			if _, err := s.AssignIP(ctx, x.f, sn, x.ip, "dhcp"); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.ConflictingIPs(ctx, sn)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string][]string{"10.0.0.5": {"alpha", "bravo"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ConflictingIPs = %v, want %v", got, want)
		}
	})
}

// The new event types are accepted by the schema.
func TestAlertEventTypesAccepted(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		for _, typ := range []string{"ip_conflict", "sync_recovered"} {
			if _, err := s.AddEvent(t.Context(), typ, nil, "x"); err != nil {
				t.Fatalf("AddEvent(%s): %v", typ, err)
			}
		}
		if _, err := s.AddEvent(t.Context(), "bogus", nil, "x"); err == nil {
			t.Fatal("unknown event type accepted; the CHECK constraint was lost")
		}
	})
}

func TestNotifySecretsEncryptedAtRest(t *testing.T) {
	eachDialectWithKey(t, testKey(), func(t *testing.T, s *Store) {
		for _, k := range []string{"notify_webhook_auth", "notify_ntfy_token"} {
			if err := s.SetSetting(t.Context(), k, "Bearer sekrit"); err != nil {
				t.Fatal(err)
			}
			raw, _ := s.rawSetting(t.Context(), k)
			if !strings.HasPrefix(raw, encPrefix) || strings.Contains(raw, "sekrit") {
				t.Fatalf("%s stored as %q", k, raw)
			}
			if got, err := s.GetSetting(t.Context(), k); err != nil || got != "Bearer sekrit" {
				t.Fatalf("%s read back %q, %v", k, got, err)
			}
		}
	})
}
