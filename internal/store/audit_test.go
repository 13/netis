package store

import (
	"fmt"
	"testing"
	"time"
)

func TestAuditLog(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		uid, err := s.CreateUser(ctx, "alice", "h", "admin")
		if err != nil {
			t.Fatal(err)
		}
		base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		for i := range 5 {
			e := AuditEntry{
				At: base.Add(time.Duration(i) * time.Minute), UserID: &uid, Username: "alice",
				Action: "device.update", Target: fmt.Sprintf("device %d", i), IP: "10.0.0.9", Status: 303,
			}
			if i%2 == 1 {
				e.UserID, e.Username, e.Action = nil, "", "login"
				e.Detail, e.Status = "unknown user", 401
			}
			if err := s.AddAudit(ctx, e); err != nil {
				t.Fatal(err)
			}
		}

		all, more, err := s.ListAudit(ctx, AuditFilter{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 5 || more {
			t.Fatalf("len=%d more=%v", len(all), more)
		}
		if all[0].Target != "device 4" || all[4].Target != "device 0" {
			t.Errorf("not newest first: %q .. %q", all[0].Target, all[4].Target)
		}
		if all[0].UserID == nil || *all[0].UserID != uid || all[0].Status != 303 || all[0].IP != "10.0.0.9" {
			t.Errorf("entry = %+v", all[0])
		}
		if !all[0].At.Equal(base.Add(4 * time.Minute)) {
			t.Errorf("at = %v", all[0].At)
		}
		if all[1].UserID != nil || all[1].Detail != "unknown user" {
			t.Errorf("anonymous entry = %+v", all[1])
		}

		// Keyset pages: two, two, one.
		page, more, err := s.ListAudit(ctx, AuditFilter{Limit: 2})
		if err != nil || len(page) != 2 || !more {
			t.Fatalf("page1 len=%d more=%v err=%v", len(page), more, err)
		}
		page, more, _ = s.ListAudit(ctx, AuditFilter{Limit: 2, BeforeID: page[1].ID})
		if len(page) != 2 || !more || page[0].Target != "device 2" {
			t.Fatalf("page2 = %+v more=%v", page, more)
		}
		page, more, _ = s.ListAudit(ctx, AuditFilter{Limit: 2, BeforeID: page[1].ID})
		if len(page) != 1 || more {
			t.Fatalf("page3 len=%d more=%v", len(page), more)
		}

		byUser, _, _ := s.ListAudit(ctx, AuditFilter{Username: "alice", Limit: 10})
		byAction, _, _ := s.ListAudit(ctx, AuditFilter{Action: "login", Limit: 10})
		if len(byUser) != 3 || len(byAction) != 2 {
			t.Errorf("byUser=%d byAction=%d", len(byUser), len(byAction))
		}

		users, actions, err := s.AuditFacets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(users) != "[alice]" || fmt.Sprint(actions) != "[device.update login]" {
			t.Errorf("users=%v actions=%v", users, actions)
		}

		// Deleting the account keeps its entries, with the name snapshot.
		if err := s.DeleteUser(ctx, uid); err != nil {
			t.Fatal(err)
		}
		byUser, _, _ = s.ListAudit(ctx, AuditFilter{Username: "alice", Limit: 10})
		if len(byUser) != 3 || byUser[0].UserID != nil {
			t.Errorf("after delete: %+v", byUser)
		}

		n, err := s.PruneAudit(ctx, base.Add(2*time.Minute))
		if err != nil || n != 2 {
			t.Fatalf("pruned %d err=%v", n, err)
		}
		all, _, _ = s.ListAudit(ctx, AuditFilter{Limit: 10})
		if len(all) != 3 {
			t.Errorf("after prune len=%d", len(all))
		}
	})
}

func TestSetUserRoleGuarded(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		a, _ := s.CreateUser(ctx, "a", "h", "admin")
		v, _ := s.CreateUser(ctx, "v", "h", "viewer")

		// The only admin cannot be demoted.
		if ok, err := s.SetUserRoleGuarded(ctx, a, "viewer"); err != nil || ok {
			t.Fatalf("demote last admin: ok=%v err=%v", ok, err)
		}
		// Promote the viewer; now either admin may be demoted.
		if ok, err := s.SetUserRoleGuarded(ctx, v, "admin"); err != nil || !ok {
			t.Fatalf("promote: ok=%v err=%v", ok, err)
		}
		if ok, err := s.SetUserRoleGuarded(ctx, a, "viewer"); err != nil || !ok {
			t.Fatalf("demote with another admin: ok=%v err=%v", ok, err)
		}
		if u, _, _ := s.GetUser(ctx, a); u.Role != "viewer" {
			t.Errorf("role = %q", u.Role)
		}
		// Setting the role a user already has is fine, even for the last admin.
		if ok, err := s.SetUserRoleGuarded(ctx, v, "admin"); err != nil || !ok {
			t.Fatalf("no-op on last admin: ok=%v err=%v", ok, err)
		}
		if ok, err := s.SetUserRoleGuarded(ctx, v, "viewer"); err != nil || ok {
			t.Fatalf("demote new last admin: ok=%v err=%v", ok, err)
		}
		if ok, err := s.SetUserRoleGuarded(ctx, v+100, "viewer"); err != nil || ok {
			t.Fatalf("missing user: ok=%v err=%v", ok, err)
		}
	})
}
