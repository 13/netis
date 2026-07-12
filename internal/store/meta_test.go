package store

import "testing"

func TestTagsFieldsLinksPortsUsersSettings(t *testing.T) {
	s := openTest(t)
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "manual"})
	ifID, _ := s.AddIface(devID, nil, nil)

	tagID, err := s.CreateTag("critical", "#ff0000")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TagDevice(devID, tagID); err != nil {
		t.Fatal(err)
	}
	names, _ := s.deviceTagNames(devID)
	if len(names) != 1 || names[0] != "critical" {
		t.Fatalf("tags=%v", names)
	}

	s.SetCustomField(devID, "rack", "u4")
	s.SetCustomField(devID, "rack", "u5") // upsert
	cfs, _ := s.ListCustomFields(devID)
	if len(cfs) != 1 || cfs[0].Value != "u5" {
		t.Fatalf("cfs=%+v", cfs)
	}

	s.AddLink(devID, "web ui", "http://10.0.0.5")
	links, _ := s.ListLinks(devID)
	if len(links) != 1 || links[0].URL != "http://10.0.0.5" {
		t.Fatalf("links=%+v", links)
	}

	s.UpsertOpenPort(ifID, 22, "tcp", "ssh", "2026-07-11T10:00:00Z")
	s.UpsertOpenPort(ifID, 22, "tcp", "ssh", "2026-07-11T11:00:00Z")
	ports, _ := s.ListOpenPorts(ifID)
	if len(ports) != 1 || ports[0].LastSeen != "2026-07-11T11:00:00Z" ||
		ports[0].FirstSeen != "2026-07-11T10:00:00Z" {
		t.Fatalf("ports=%+v", ports)
	}

	if n, _ := s.CountUsers(); n != 0 {
		t.Fatal("expected 0 users")
	}
	s.CreateUser("ben", "hash", "admin")
	u, ok, _ := s.GetUserByName("ben")
	if !ok || u.Role != "admin" {
		t.Fatalf("user=%+v ok=%v", u, ok)
	}
	s.CreateSession("tok1", u.ID, "2099-01-01T00:00:00Z")
	su, ok, _ := s.GetSession("tok1")
	if !ok || su.Username != "ben" {
		t.Fatalf("session user=%+v ok=%v", su, ok)
	}
	s.CreateSession("tok2", u.ID, "2000-01-01T00:00:00Z")
	if _, ok, _ := s.GetSession("tok2"); ok {
		t.Fatal("expired session should not resolve")
	}

	if v, _ := s.GetSetting("nope"); v != "" {
		t.Fatal("missing setting should be empty")
	}
	s.SetSetting("offline_after", "3")
	s.SetSetting("offline_after", "4")
	if v, _ := s.GetSetting("offline_after"); v != "4" {
		t.Fatalf("setting=%q", v)
	}
}

func TestSetDeviceTags(t *testing.T) {
	s := openTest(t)
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "manual"})

	names := func() []string {
		tags, _ := s.DeviceTags(devID)
		out := make([]string, 0, len(tags))
		for _, tg := range tags {
			out = append(out, tg.Name)
		}
		return out
	}

	// Attach two, creating tags that don't exist. Input has dupes/blanks/spaces.
	if err := s.SetDeviceTags(devID, []string{"web", " web ", "", "db"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 2 || got[0] != "db" || got[1] != "web" {
		t.Fatalf("after first sync tags=%v, want [db web]", got)
	}

	// Re-sync to a set that drops "db", keeps "web", adds "nas".
	if err := s.SetDeviceTags(devID, []string{"web", "nas"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 2 || got[0] != "nas" || got[1] != "web" {
		t.Fatalf("after second sync tags=%v, want [nas web]", got)
	}

	// Empty set detaches everything.
	if err := s.SetDeviceTags(devID, []string{"  ", ""}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 0 {
		t.Fatalf("after clear tags=%v, want []", got)
	}

	// A tag reused across syncs is not duplicated in the tag table.
	all, _ := s.ListTags()
	seen := map[string]int{}
	for _, tg := range all {
		seen[tg.Name]++
	}
	if seen["web"] != 1 {
		t.Fatalf("tag 'web' should exist exactly once, got %d", seen["web"])
	}
}

func TestDeleteUserGuardedKeepsLastAdmin(t *testing.T) {
	s := openTest(t)

	adminID, err := s.CreateUser("admin1", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Sole admin: guard must refuse.
	deleted, err := s.DeleteUserGuarded(adminID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("expected sole admin to survive DeleteUserGuarded")
	}
	if _, ok, _ := s.GetUserByName("admin1"); !ok {
		t.Fatal("sole admin was deleted")
	}

	// Add a second admin: now one of them can be deleted.
	admin2ID, err := s.CreateUser("admin2", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err = s.DeleteUserGuarded(admin2ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected second admin deletion to succeed")
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("expected 1 user remaining, got %d", n)
	}

	// Back down to a single admin: guard must refuse again.
	deleted, err = s.DeleteUserGuarded(adminID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("expected last remaining admin to survive DeleteUserGuarded")
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("expected 1 user remaining, got %d", n)
	}
}
