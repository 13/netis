package store

import "testing"

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
