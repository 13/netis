package store

import (
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
