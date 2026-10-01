package store

import (
	"testing"
	"time"
)

func TestAPITokenLifecycle(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		ben, err := s.CreateUser(ctx, "ben", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		eve, err := s.CreateUser(ctx, "eve", "h", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		benTok, err := s.CreateAPIToken(ctx, ben, "backup script", "netis_ben", "", false, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateAPIToken(ctx, eve, "grafana", "netis_eve", now.Add(time.Hour).Format(time.RFC3339), false, now); err != nil {
			t.Fatal(err)
		}

		// The token resolves to its owner, and only the digest is stored.
		u, ok, err := s.GetUserByAPIToken(ctx, "netis_ben", now)
		if err != nil || !ok || u.ID != ben || u.Role != "admin" {
			t.Fatalf("lookup: u=%+v ok=%v err=%v", u, ok, err)
		}
		var stored string
		if err := s.queryRow(ctx, `SELECT token_hash FROM api_token WHERE id=?`, benTok).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == "netis_ben" || stored != hashToken("netis_ben") {
			t.Fatalf("token_hash = %q, want the sha256 of the token", stored)
		}
		if _, ok, _ := s.GetUserByAPIToken(ctx, "netis_nope", now); ok {
			t.Fatal("unknown token resolved")
		}

		// Listing: own tokens for a user, all of them for an admin (zero).
		mine, err := s.ListAPITokens(ctx, eve)
		if err != nil || len(mine) != 1 || mine[0].Name != "grafana" || mine[0].Username != "eve" {
			t.Fatalf("eve's tokens = %+v err=%v", mine, err)
		}
		all, err := s.ListAPITokens(ctx, 0)
		if err != nil || len(all) != 2 {
			t.Fatalf("all tokens = %+v err=%v", all, err)
		}
		for _, tok := range all {
			if tok.ID == benTok && (tok.LastUsedAt == nil || *tok.LastUsedAt != now.Format(time.RFC3339)) {
				t.Errorf("last_used_at = %v after use", tok.LastUsedAt)
			}
		}

		// An expired token is refused and swept by retention.
		if _, ok, _ := s.GetUserByAPIToken(ctx, "netis_eve", now.Add(2*time.Hour)); ok {
			t.Fatal("expired token resolved")
		}
		n, err := s.PruneExpiredAPITokens(ctx, now.Add(2*time.Hour))
		if err != nil || n != 1 {
			t.Fatalf("pruned %d err=%v, want 1", n, err)
		}

		// Revoking is scoped: eve cannot delete ben's token, an admin (0) can.
		if ok, err := s.DeleteAPIToken(ctx, eve, benTok); err != nil || ok {
			t.Fatalf("eve deleted ben's token: ok=%v err=%v", ok, err)
		}
		if ok, err := s.DeleteAPIToken(ctx, 0, benTok); err != nil || !ok {
			t.Fatalf("admin delete: ok=%v err=%v", ok, err)
		}
		if _, ok, _ := s.GetUserByAPIToken(ctx, "netis_ben", now); ok {
			t.Fatal("revoked token still resolves")
		}
	})
}

// last_used_at moves at most once a minute, so a polling script is not a
// write per request.
func TestAPITokenLastUsedThrottled(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		t0 := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		uid, _ := s.CreateUser(ctx, "ben", "h", "admin")
		if _, err := s.CreateAPIToken(ctx, uid, "x", "netis_x", "", false, t0); err != nil {
			t.Fatal(err)
		}
		lastUsed := func() string {
			var v *string
			if err := s.queryRow(ctx, `SELECT last_used_at FROM api_token`).Scan(&v); err != nil {
				t.Fatal(err)
			}
			if v == nil {
				return ""
			}
			return *v
		}
		for _, step := range []struct {
			at   time.Time
			want time.Time
		}{
			{t0, t0},
			{t0.Add(30 * time.Second), t0},
			{t0.Add(59 * time.Second), t0},
			{t0.Add(61 * time.Second), t0.Add(61 * time.Second)},
		} {
			if _, ok, err := s.GetUserByAPIToken(ctx, "netis_x", step.at); err != nil || !ok {
				t.Fatalf("lookup at %v: ok=%v err=%v", step.at, ok, err)
			}
			if got := lastUsed(); got != step.want.Format(time.RFC3339) {
				t.Errorf("after use at %v: last_used_at=%s, want %s", step.at, got, step.want.Format(time.RFC3339))
			}
		}
	})
}

// Deleting a user takes their tokens with them.
func TestAPITokensDeletedWithUser(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		now := time.Now()
		if _, err := s.CreateUser(ctx, "admin", "h", "admin"); err != nil {
			t.Fatal(err)
		}
		uid, _ := s.CreateUser(ctx, "eve", "h", "viewer")
		if _, err := s.CreateAPIToken(ctx, uid, "x", "netis_eve", "", false, now); err != nil {
			t.Fatal(err)
		}
		if ok, err := s.DeleteUserGuarded(ctx, uid); err != nil || !ok {
			t.Fatalf("delete user: ok=%v err=%v", ok, err)
		}
		if _, ok, _ := s.GetUserByAPIToken(ctx, "netis_eve", now); ok {
			t.Fatal("token of a deleted user still resolves")
		}
		var n int
		if err := s.queryRow(ctx, `SELECT count(*) FROM api_token`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("api_token rows = %d err=%v", n, err)
		}
	})
}

func TestPruneCountsAPITokens(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		now := time.Now()
		uid, _ := s.CreateUser(ctx, "ben", "h", "admin")
		s.CreateAPIToken(ctx, uid, "old", "netis_old", now.Add(-time.Hour).UTC().Format(time.RFC3339), false, now.Add(-2*time.Hour))
		s.CreateAPIToken(ctx, uid, "forever", "netis_forever", "", false, now)
		r, err := s.Prune(ctx, now, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r.APITokens != 1 || r.Total() != 1 {
			t.Fatalf("prune = %+v", r)
		}
		if _, ok, _ := s.GetUserByAPIToken(ctx, "netis_forever", now); !ok {
			t.Fatal("a token with no expiry was pruned")
		}
	})
}

// A token is full or read-only; the lookup and the token list both say which.
func TestAPITokenReadOnly(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		uid, err := s.CreateUser(ctx, "ben", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateAPIToken(ctx, uid, "grafana", "netis_ro", "", true, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateAPIToken(ctx, uid, "ansible", "netis_rw", "", false, now); err != nil {
			t.Fatal(err)
		}
		for tok, want := range map[string]bool{"netis_ro": true, "netis_rw": false} {
			u, ok, err := s.GetUserByAPIToken(ctx, tok, now)
			if err != nil || !ok || u.ID != uid || u.ReadOnly != want {
				t.Errorf("%s: u=%+v ok=%v err=%v, want read-only %v", tok, u, ok, err, want)
			}
		}
		list, err := s.ListAPITokens(ctx, uid)
		if err != nil || len(list) != 2 {
			t.Fatalf("list = %+v err=%v", list, err)
		}
		for _, tok := range list {
			if tok.ReadOnly != (tok.Name == "grafana") {
				t.Errorf("token %q read-only = %v", tok.Name, tok.ReadOnly)
			}
		}
	})
}
