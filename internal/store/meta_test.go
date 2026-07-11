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
