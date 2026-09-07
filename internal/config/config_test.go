package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NETIS_ADDR", "")
	t.Setenv("NETIS_DB", "")
	c := Load()
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DSN != "netis.db" {
		t.Errorf("DSN = %q, want netis.db", c.DSN)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("NETIS_ADDR", ":9999")
	t.Setenv("NETIS_DB", "/data/n.db")
	c := Load()
	if c.Addr != ":9999" || c.DSN != "/data/n.db" {
		t.Errorf("got %+v", c)
	}
}

// NETIS_DB carries a Postgres URL as-is; selecting the backend from it is the
// store's job, so config must not rewrite or validate it.
func TestLoadPostgresDSN(t *testing.T) {
	t.Setenv("NETIS_DB", "postgres://u:p@db:5432/netis?sslmode=disable")
	c := Load()
	if c.DSN != "postgres://u:p@db:5432/netis?sslmode=disable" {
		t.Errorf("DSN = %q", c.DSN)
	}
}
