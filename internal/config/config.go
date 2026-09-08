package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr string
	// DSN names the database: a Postgres URL (postgres://…) selects the
	// Postgres backend, anything else is a SQLite file path.
	DSN string
	// MaxOpenConns and MaxIdleConns size the Postgres connection pool. Zero
	// means "use the store's default"; both are ignored on SQLite, which is
	// held to one connection on purpose.
	MaxOpenConns int
	MaxIdleConns int
}

func Load() Config {
	c := Config{Addr: ":8080", DSN: "netis.db"}
	if v := os.Getenv("NETIS_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("NETIS_DB"); v != "" {
		c.DSN = v
	}
	c.MaxOpenConns = envInt("NETIS_DB_MAX_OPEN_CONNS")
	c.MaxIdleConns = envInt("NETIS_DB_MAX_IDLE_CONNS")
	return c
}

// envInt reads a positive integer from the environment, returning 0 for unset,
// unparseable or non-positive values so the caller falls back to its default.
func envInt(key string) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
