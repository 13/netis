package config

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

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

func TestParseTrustedProxies(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"10.0.0.0/8", []string{"10.0.0.0/8"}},
		{"127.0.0.1", []string{"127.0.0.1/32"}},
		{"::1", []string{"::1/128"}},
		{"10.0.0.0/8, 192.168.1.5 ,fd00::/8", []string{"10.0.0.0/8", "192.168.1.5/32", "fd00::/8"}},
	}
	for _, c := range cases {
		got, err := ParseTrustedProxies(c.in)
		if err != nil {
			t.Errorf("ParseTrustedProxies(%q): %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("ParseTrustedProxies(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i].String() != c.want[i] {
				t.Errorf("ParseTrustedProxies(%q)[%d] = %s, want %s", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// A typo in the proxy list must stop the process rather than silently leaving
// every forwarded header untrusted (or, worse, trusted).
func TestParseTrustedProxiesRejectsGarbage(t *testing.T) {
	for _, in := range []string{"nonsense", "10.0.0.0/33", "10.0.0.0/8,oops", "example.com"} {
		if _, err := ParseTrustedProxies(in); err == nil {
			t.Errorf("ParseTrustedProxies(%q): want error", in)
		}
	}
}

func TestLoadReadsTrustedProxies(t *testing.T) {
	t.Setenv("NETIS_TRUSTED_PROXIES", "10.0.0.0/8")
	if c := Load(); c.TrustedProxies != "10.0.0.0/8" {
		t.Errorf("TrustedProxies = %q", c.TrustedProxies)
	}
}

func TestParseSecretKey(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	b64 := base64.StdEncoding.EncodeToString(key)
	hexKey := hex.EncodeToString(key)

	for _, in := range []string{b64, hexKey, "  " + b64 + "  ", strings.TrimRight(b64, "=")} {
		got, err := ParseSecretKey(in)
		if err != nil {
			t.Fatalf("ParseSecretKey(%q): %v", in, err)
		}
		if !bytes.Equal(got, key) {
			t.Errorf("ParseSecretKey(%q) = %x", in, got)
		}
	}

	if got, err := ParseSecretKey(""); err != nil || got != nil {
		t.Errorf("empty key: got=%v err=%v", got, err)
	}

	// Wrong lengths and undecodable values must fail rather than being padded
	// into a key nobody can reproduce.
	for _, in := range []string{
		"short",
		base64.StdEncoding.EncodeToString(make([]byte, 16)),
		base64.StdEncoding.EncodeToString(make([]byte, 64)),
		"!!!not base64 or hex!!!",
	} {
		if _, err := ParseSecretKey(in); err == nil {
			t.Errorf("ParseSecretKey(%q): want error", in)
		}
	}
}
