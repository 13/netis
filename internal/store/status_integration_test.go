package store

import "testing"

func TestIntegrationStatusUpsert(t *testing.T) {
	s := openTest(t)
	if err := s.SetIntegrationStatus(t.Context(), IntegrationStatus{
		Name: "pihole", LastRun: "2026-07-11T10:00:00Z", OK: true, Detail: "48 leases", ItemCount: 48,
	}); err != nil {
		t.Fatal(err)
	}
	// Upsert same name: update in place, no duplicate row.
	if err := s.SetIntegrationStatus(t.Context(), IntegrationStatus{
		Name: "pihole", LastRun: "2026-07-11T10:05:00Z", OK: false, Detail: "auth failed", ItemCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListIntegrationStatus(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	got := list[0]
	if got.Name != "pihole" || got.OK != false || got.Detail != "auth failed" ||
		got.LastRun != "2026-07-11T10:05:00Z" || got.ItemCount != 0 {
		t.Fatalf("row not updated: %+v", got)
	}
}

func TestIntegrationStatusOrderedByName(t *testing.T) {
	s := openTest(t)
	for _, n := range []string{"scan", "proxmox", "wireguard"} {
		if err := s.SetIntegrationStatus(t.Context(), IntegrationStatus{Name: n, LastRun: "2026-07-11T10:00:00Z", OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.ListIntegrationStatus(t.Context())
	if len(list) != 3 || list[0].Name != "proxmox" || list[1].Name != "scan" || list[2].Name != "wireguard" {
		t.Fatalf("order wrong: %+v", list)
	}
}
