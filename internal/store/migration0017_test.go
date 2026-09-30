package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// A tag created by SetDeviceTags has no colour of its own: the empty string
// means auto.
func TestConformanceNewTagColorIsAuto(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		devID, err := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetDeviceTags(ctx, devID, []string{"nas"}); err != nil {
			t.Fatal(err)
		}
		all, err := s.ListTags(ctx)
		if err != nil || len(all) != 1 || all[0].Name != "nas" || all[0].Color != "" {
			t.Fatalf("ListTags = %+v err=%v, want nas with color ''", all, err)
		}
	})
}

// 0017 turns every colour that is not a palette key into the empty string
// (auto) and keeps palette keys.
func TestMigration0017TagColors(t *testing.T) {
	const seed = `INSERT INTO tag (id,name,color) VALUES (1,'grey','#888888');
		INSERT INTO tag (id,name,color) VALUES (2,'nas','teal');
		INSERT INTO tag (id,name,color) VALUES (3,'odd','Teal');`
	check := func(t *testing.T, s *Store) {
		all, err := s.ListTags(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, tg := range all {
			got[tg.Name] = tg.Color
		}
		want := map[string]string{"grey": "", "nas": "teal", "odd": ""}
		for n, c := range want {
			if got[n] != c {
				t.Errorf("tag %s color = %q, want %q", n, got[n], c)
			}
		}
	}

	t.Run("sqlite", func(t *testing.T) {
		path := t.TempDir() + "/mig.db"
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		applyMigrationsBefore(t, db, "sqlite", "0017")
		if _, err := db.Exec(seed); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open (runs 0017): %v", err)
		}
		defer s.Close()
		check(t, s)
	})

	dsn := os.Getenv("NETIS_TEST_PG_DSN")
	if dsn == "" {
		t.Log("NETIS_TEST_PG_DSN not set; skipping postgres")
		return
	}
	t.Run("postgres", func(t *testing.T) {
		schema := fmt.Sprintf("netis_test_%d", time.Now().UnixNano())
		admin, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		scoped := dsn + sep + "search_path=" + schema
		db, err := sql.Open("pgx", scoped)
		if err != nil {
			t.Fatal(err)
		}
		applyMigrationsBefore(t, db, "postgres", "0017")
		if _, err := db.Exec(seed); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(scoped)
		if err != nil {
			t.Fatalf("Open (runs 0017): %v", err)
		}
		defer s.Close()
		check(t, s)
	})
}
