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

func TestLoadPoolSizes(t *testing.T) {
	// Unset means "let the store choose", not zero connections.
	c := Load()
	if c.MaxOpenConns != 0 || c.MaxIdleConns != 0 {
		t.Errorf("unset pool sizes = %d/%d, want 0/0", c.MaxOpenConns, c.MaxIdleConns)
	}
	t.Setenv("NETIS_DB_MAX_OPEN_CONNS", "25")
	t.Setenv("NETIS_DB_MAX_IDLE_CONNS", "5")
	c = Load()
	if c.MaxOpenConns != 25 || c.MaxIdleConns != 5 {
		t.Errorf("pool sizes = %d/%d, want 25/5", c.MaxOpenConns, c.MaxIdleConns)
	}
	// Garbage and non-positive values fall back rather than crippling the pool.
	for _, bad := range []string{"lots", "0", "-3"} {
		t.Setenv("NETIS_DB_MAX_OPEN_CONNS", bad)
		if c := Load(); c.MaxOpenConns != 0 {
			t.Errorf("NETIS_DB_MAX_OPEN_CONNS=%q gave %d, want 0", bad, c.MaxOpenConns)
		}
	}
}
