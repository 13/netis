package store

import "testing"

func TestOIDCIdentity(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		id, err := s.CreateOIDCUser(ctx, "alice", "h", "viewer", "https://idp", "sub-1")
		if err != nil {
			t.Fatal(err)
		}
		u, ok, err := s.GetUserByOIDC(ctx, "https://idp", "sub-1")
		if err != nil || !ok || u.ID != id || u.Username != "alice" {
			t.Fatalf("GetUserByOIDC = %+v ok=%v err=%v", u, ok, err)
		}
		// The subject alone is not an identity: another issuer's sub-1 is
		// somebody else.
		if _, ok, err := s.GetUserByOIDC(ctx, "https://other", "sub-1"); err != nil || ok {
			t.Fatalf("matched across issuers: ok=%v err=%v", ok, err)
		}
		if linked, err := s.OIDCLinked(ctx, id); err != nil || !linked {
			t.Fatalf("OIDCLinked = %v, %v", linked, err)
		}

		// Password-only accounts can coexist with NULL identities.
		bob, err := s.CreateUser(ctx, "bob", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateUser(ctx, "carol", "h", "viewer"); err != nil {
			t.Fatal(err)
		}
		if linked, err := s.OIDCLinked(ctx, bob); err != nil || linked {
			t.Fatalf("OIDCLinked(bob) = %v, %v", linked, err)
		}

		// One identity cannot be linked to two users.
		if _, err := s.LinkOIDC(ctx, bob, "https://idp", "sub-1"); !IsUniqueViolation(err) {
			t.Fatalf("second link of the same identity: err=%v, want unique violation", err)
		}
		if ok, err := s.LinkOIDC(ctx, bob, "https://idp", "sub-2"); err != nil || !ok {
			t.Fatalf("LinkOIDC = %v, %v", ok, err)
		}
		if ok, err := s.LinkOIDC(ctx, bob+999, "https://idp", "sub-3"); err != nil || ok {
			t.Fatalf("LinkOIDC on a missing user = %v, %v", ok, err)
		}

		if err := s.SetSSORole(ctx, id, "admin"); err != nil {
			t.Fatal(err)
		}
		if u, _, _ := s.GetUser(ctx, id); u.Role != "admin" {
			t.Fatalf("role = %q after SetSSORole", u.Role)
		}
	})
}
