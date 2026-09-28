// Package storetest opens stores for tests outside the store package, so code
// that drives the store (the integration syncs, for one) can be exercised on
// both dialects the way the store's own conformance tests are.
package storetest

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// EachDialect runs fn against an in-memory SQLite store, and again against a
// scratch Postgres schema when NETIS_TEST_PG_DSN points at a server.
func EachDialect(t *testing.T, fn func(t *testing.T, st *store.Store)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		st, err := store.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		fn(t, st)
	})

	dsn := os.Getenv("NETIS_TEST_PG_DSN")
	if dsn == "" {
		t.Log("NETIS_TEST_PG_DSN not set; skipping postgres")
		return
	}
	t.Run("postgres", func(t *testing.T) {
		fn(t, openPGSchema(t, dsn))
	})
}

// openPGSchema creates a scratch schema on the server named by dsn, returns a
// store whose search_path points at it, and drops the schema afterwards.
func openPGSchema(t *testing.T, dsn string) *store.Store {
	t.Helper()
	schema := fmt.Sprintf("netis_st_%d", time.Now().UnixNano())

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	st, err := store.Open(dsn + sep + "search_path=" + schema)
	if err != nil {
		t.Fatalf("open postgres schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		st.Close()
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer db.Close()
		db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	})
	return st
}
