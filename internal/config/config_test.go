package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NETIS_ADDR", "")
	t.Setenv("NETIS_DB", "")
	c := Load()
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DBPath != "netis.db" {
		t.Errorf("DBPath = %q, want netis.db", c.DBPath)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("NETIS_ADDR", ":9999")
	t.Setenv("NETIS_DB", "/data/n.db")
	c := Load()
	if c.Addr != ":9999" || c.DBPath != "/data/n.db" {
		t.Errorf("got %+v", c)
	}
}
