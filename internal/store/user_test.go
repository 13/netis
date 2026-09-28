package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSetPasswordHash(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, err := s.CreateUser(t.Context(), "ben", "old-hash", "admin")
		if err != nil {
			t.Fatal(err)
		}
		updated, err := s.SetPasswordHash(t.Context(), id, "new-hash")
		if err != nil {
			t.Fatal(err)
		}
		if !updated {
			t.Fatal("SetPasswordHash reported no row updated")
		}
		u, ok, err := s.GetUserByName(t.Context(), "ben")
		if err != nil || !ok {
			t.Fatalf("GetUserByName: ok=%v err=%v", ok, err)
		}
		if u.PasswordHash != "new-hash" {
			t.Fatalf("PasswordHash = %q", u.PasswordHash)
		}
		// A missing user is reported, not silently treated as a success: the
		// handler tells the caller their reset did nothing.
		if updated, err := s.SetPasswordHash(t.Context(), id+999, "x"); err != nil || updated {
			t.Fatalf("SetPasswordHash on a missing user: updated=%v err=%v", updated, err)
		}
	})
}

func TestGetUser(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, err := s.CreateUser(t.Context(), "ben", "h", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		u, ok, err := s.GetUser(t.Context(), id)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if u.Username != "ben" || u.Role != "viewer" {
			t.Fatalf("user=%+v", u)
		}
		if _, ok, err := s.GetUser(t.Context(), id+999); err != nil || ok {
			t.Fatalf("missing user: ok=%v err=%v", ok, err)
		}
	})
}

// A changed password must not leave old sessions usable — that is the whole
// point of changing it after a leak. The session doing the change is kept so
// the user isn't logged out of the browser they're sitting at.
func TestDeleteSessionsForUser(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, err := s.CreateUser(t.Context(), "ben", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		other, err := s.CreateUser(t.Context(), "kim", "h", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		expires := "2999-01-01T00:00:00Z"
		for _, tok := range []string{"tok-a", "tok-b", "tok-c"} {
			if err := s.CreateSession(t.Context(), tok, id, expires); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.CreateSession(t.Context(), "tok-other", other, expires); err != nil {
			t.Fatal(err)
		}

		if err := s.DeleteSessionsForUser(t.Context(), id, "tok-b"); err != nil {
			t.Fatal(err)
		}
		for _, tok := range []string{"tok-a", "tok-c"} {
			if _, ok, _ := s.GetSession(t.Context(), tok); ok {
				t.Errorf("session %s should be revoked", tok)
			}
		}
		if _, ok, _ := s.GetSession(t.Context(), "tok-b"); !ok {
			t.Error("the session named as kept should survive")
		}
		if _, ok, _ := s.GetSession(t.Context(), "tok-other"); !ok {
			t.Error("another user's session must not be touched")
		}

		// An empty keep token revokes every session for the user, which is what
		// an admin resetting someone else's password needs.
		if err := s.DeleteSessionsForUser(t.Context(), id, ""); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := s.GetSession(t.Context(), "tok-b"); ok {
			t.Error("tok-b should be revoked with no keep token")
		}
	})
}

func TestListAndRevokeSessions(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, err := s.CreateUser(t.Context(), "ben", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		other, err := s.CreateUser(t.Context(), "kim", "h", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		future := "2999-01-01T00:00:00Z"
		if err := s.CreateSession(t.Context(), "newer", id, future, SessionMeta{
			CreatedAt: "2026-09-13T10:00:00Z", IP: "10.0.0.5", UserAgent: "Firefox",
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(t.Context(), "older", id, future, SessionMeta{
			CreatedAt: "2026-09-01T10:00:00Z", IP: "10.0.0.6", UserAgent: "curl",
		}); err != nil {
			t.Fatal(err)
		}
		// No metadata at all: a session from before it was recorded.
		if err := s.CreateSession(t.Context(), "legacy", id, future); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(t.Context(), "expired", id, "2000-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(t.Context(), "kims", other, future); err != nil {
			t.Fatal(err)
		}

		list, err := s.ListSessionsForUser(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 3 {
			t.Fatalf("sessions=%+v, want the 3 unexpired ones", list)
		}
		if list[0].ID != SessionID("newer") || list[1].ID != SessionID("older") {
			t.Errorf("newest should sort first: %+v", list)
		}
		if list[2].ID != SessionID("legacy") || list[2].CreatedAt != nil {
			t.Errorf("a session with no created_at should sort last: %+v", list[2])
		}
		if list[0].IP == nil || *list[0].IP != "10.0.0.5" ||
			list[0].UserAgent == nil || *list[0].UserAgent != "Firefox" {
			t.Errorf("metadata not returned: %+v", list[0])
		}

		deleted, err := s.DeleteSessionByID(t.Context(), id, SessionID("older"))
		if err != nil || !deleted {
			t.Fatalf("revoke: deleted=%v err=%v", deleted, err)
		}
		if _, ok, _ := s.GetSession(t.Context(), "older"); ok {
			t.Error("revoked session should be gone")
		}

		// Another user's session is not revocable even with its id in hand.
		deleted, err = s.DeleteSessionByID(t.Context(), id, SessionID("kims"))
		if err != nil {
			t.Fatal(err)
		}
		if deleted {
			t.Error("must not revoke another user's session")
		}
		if _, ok, _ := s.GetSession(t.Context(), "kims"); !ok {
			t.Error("another user's session should survive")
		}
		if deleted, err := s.DeleteSessionByID(t.Context(), id, "nosuchid"); err != nil || deleted {
			t.Errorf("unknown id: deleted=%v err=%v", deleted, err)
		}
	})
}

// SessionID must not leak the token it names.
func TestSessionIDIsADigest(t *testing.T) {
	id := SessionID("super-secret-token")
	if id == "" || len(id) != 16 {
		t.Fatalf("SessionID = %q", id)
	}
	if strings.Contains(id, "secret") {
		t.Fatal("SessionID leaks the token")
	}
	if SessionID("super-secret-token") != id {
		t.Fatal("SessionID must be stable")
	}
	if SessionID("another-token") == id {
		t.Fatal("SessionID must differ per token")
	}
}

// A session row is a bearer credential at rest. Only a digest of the token is
// stored, so a leaked database file or backup does not hand out live sessions,
// and every lookup still works from the raw token the cookie carries.
func TestSessionTokenStoredHashed(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, err := s.CreateUser(t.Context(), "ben", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		expires := "2999-01-01T00:00:00Z"
		if err := s.CreateSession(t.Context(), "raw-token", id, expires); err != nil {
			t.Fatal(err)
		}
		var stored string
		if err := s.queryRow(t.Context(), `SELECT token_hash FROM session`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stored, "raw-token") {
			t.Fatalf("session table holds the raw token: %q", stored)
		}
		sum := sha256.Sum256([]byte("raw-token"))
		if want := hex.EncodeToString(sum[:]); stored != want {
			t.Fatalf("stored %q, want sha256 %q", stored, want)
		}
		if _, ok, err := s.GetSession(t.Context(), "raw-token"); err != nil || !ok {
			t.Fatalf("GetSession: ok=%v err=%v", ok, err)
		}
		// Presenting the stored digest as a cookie must not work: it is not
		// the credential, only a record of it.
		if _, ok, _ := s.GetSession(t.Context(), stored); ok {
			t.Fatal("the stored digest authenticates as a token")
		}

		sessions, err := s.ListSessionsForUser(t.Context(), id)
		if err != nil || len(sessions) != 1 || sessions[0].ID != SessionID("raw-token") {
			t.Fatalf("ListSessionsForUser = %+v, %v", sessions, err)
		}
		if deleted, err := s.DeleteSessionByID(t.Context(), id+1, SessionID("raw-token")); err != nil || deleted {
			t.Fatalf("revoke by another user: deleted=%v err=%v", deleted, err)
		}
		if deleted, err := s.DeleteSessionByID(t.Context(), id, SessionID("raw-token")); err != nil || !deleted {
			t.Fatalf("revoke by owner: deleted=%v err=%v", deleted, err)
		}
		if _, ok, _ := s.GetSession(t.Context(), "raw-token"); ok {
			t.Fatal("revoked session still resolves")
		}
	})
}

// Sessions written before tokens were hashed hold the raw token, which no
// longer matches any lookup. Migration 0008 drops them rather than leave
// plaintext credentials sitting in the table; their owners sign in again.
func TestMigration0008DropsPlaintextSessions(t *testing.T) {
	path := t.TempDir() + "/mig.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	names := []string{"0001_init.sql", "0002_pihole_source.sql", "0003_integration_status.sql",
		"0004_device_reviewed.sql", "0005_device_model_function.sql", "0006_lookup_indexes.sql",
		"0007_session_metadata.sql"}
	for _, name := range names {
		b, err := migrationsFS.ReadFile("migrations/sqlite/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO user (id,username,password_hash,role) VALUES (1,'ben','h','admin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session (token,user_id,expires_at) VALUES ('plain',1,'2999-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open (runs 0008): %v", err)
	}
	defer s.Close()
	var n int
	if err := s.queryRow(t.Context(), `SELECT count(*) FROM session`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d plaintext session(s) survived the migration", n)
	}
}
