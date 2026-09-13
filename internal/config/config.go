package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
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
	// TrustedProxies is the raw NETIS_TRUSTED_PROXIES value: a comma-separated
	// list of CIDRs or bare addresses. Parse it with ParseTrustedProxies.
	TrustedProxies string
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
	c.TrustedProxies = os.Getenv("NETIS_TRUSTED_PROXIES")
	return c
}

// ParseTrustedProxies parses a comma-separated list of CIDRs and bare
// addresses into prefixes. A bare address becomes a single-host prefix, so
// "127.0.0.1" and "127.0.0.1/32" mean the same thing.
//
// Nothing is skipped silently: a malformed entry is an error the caller is
// expected to exit on. A mistyped proxy list that quietly parsed to nothing
// would leave netis reading X-Forwarded-For from no one — the failure mode is
// a rate limiter that buckets every login behind a reverse proxy together,
// which is invisible until someone is locked out.
func ParseTrustedProxies(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if strings.Contains(field, "/") {
			p, err := netip.ParsePrefix(field)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", field, err)
			}
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(field)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", field, err)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
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
