package config

import "os"

type Config struct {
	Addr string
	// DSN names the database: a Postgres URL (postgres://…) selects the
	// Postgres backend, anything else is a SQLite file path.
	DSN string
}

func Load() Config {
	c := Config{Addr: ":8080", DSN: "netis.db"}
	if v := os.Getenv("NETIS_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("NETIS_DB"); v != "" {
		c.DSN = v
	}
	return c
}
