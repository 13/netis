package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/store"
)

// postAs submits a form as the session token given, which is what lets these
// tests act as a second browser or as a non-admin user.
func postAs(t *testing.T, srv *Server, token, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: token})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// addUser creates a user with a known password and a session for them.
func addUser(t *testing.T, st *store.Store, name, password, role, token string) int64 {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateUser(t.Context(), name, string(h), role)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(t.Context(), token, id,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	return id
}

func passwordWorks(t *testing.T, st *store.Store, name, password string) bool {
	t.Helper()
	u, ok, err := st.GetUserByName(t.Context(), name)
	if err != nil || !ok {
		t.Fatalf("GetUserByName(%q): ok=%v err=%v", name, ok, err)
	}
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

func TestUserChangesOwnPassword(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st) // keeps an admin around so roles aren't the thing under test
	// A viewer, deliberately: rotating your own credential must not need admin.
	addUser(t, st, "kim", "old-password", "viewer", "kim-here")
	addUser(t, st, "kim-elsewhere", "irrelevant", "viewer", "unused")
	// A second session for kim, standing in for another browser or device.
	u, _, _ := st.GetUserByName(t.Context(), "kim")
	st.CreateSession(t.Context(), "kim-other", u.ID,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339))

	rec := postAs(t, srv, "kim-here", "/settings/password", url.Values{
		"current_password": {"old-password"},
		"new_password":     {"new-password"},
		"confirm_password": {"new-password"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !passwordWorks(t, st, "kim", "new-password") {
		t.Error("new password should authenticate")
	}
	if passwordWorks(t, st, "kim", "old-password") {
		t.Error("old password should no longer authenticate")
	}
	if _, ok, _ := st.GetSession(t.Context(), "kim-other"); ok {
		t.Error("kim's other session should have been revoked")
	}
	if _, ok, _ := st.GetSession(t.Context(), "kim-here"); !ok {
		t.Error("the session that made the change should survive")
	}
	if _, ok, _ := st.GetSession(t.Context(), "unused"); !ok {
		t.Error("another user's session must not be revoked")
	}
}

func TestPasswordChangeRejections(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		code int
	}{
		{"wrong current password", url.Values{
			"current_password": {"not-it"},
			"new_password":     {"new-password"},
			"confirm_password": {"new-password"},
		}, http.StatusForbidden},
		{"confirmation does not match", url.Values{
			"current_password": {"old-password"},
			"new_password":     {"new-password"},
			"confirm_password": {"new-passwordd"},
		}, http.StatusBadRequest},
		{"too short", url.Values{
			"current_password": {"old-password"},
			"new_password":     {"sh0rt"},
			"confirm_password": {"sh0rt"},
		}, http.StatusBadRequest},
		{"empty", url.Values{
			"current_password": {"old-password"},
		}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, st := testServer(t)
			st.SetSetting(t.Context(), "onboarded", "1")
			addUser(t, st, "kim", "old-password", "viewer", "kim-here")
			rec := postAs(t, srv, "kim-here", "/settings/password", c.form)
			if rec.Code != c.code {
				t.Fatalf("code=%d want %d body=%s", rec.Code, c.code, rec.Body.String())
			}
			if !passwordWorks(t, st, "kim", "old-password") {
				t.Error("a rejected change must leave the password alone")
			}
			if _, ok, _ := st.GetSession(t.Context(), "kim-here"); !ok {
				t.Error("a rejected change must not revoke sessions")
			}
		})
	}
}

func TestAdminResetsAnotherUsersPassword(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	kimID := addUser(t, st, "kim", "old-password", "viewer", "kim-session")

	rec := authedPost(t, srv, st, "/settings/users/"+strconv.FormatInt(kimID, 10)+"/password",
		url.Values{"new_password": {"issued-password"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !passwordWorks(t, st, "kim", "issued-password") {
		t.Error("the issued password should authenticate")
	}
	// Nothing was kept for kim: a reset exists because the old credential is
	// not trusted any more, so every session made with it goes.
	if _, ok, _ := st.GetSession(t.Context(), "kim-session"); ok {
		t.Error("the reset user's sessions should all be revoked")
	}
	if _, ok, _ := st.GetSession(t.Context(), "testtok"); !ok {
		t.Error("the admin's own session must survive")
	}
}

func TestAdminResettingOwnPasswordKeepsCurrentSession(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec0 := authedGet(t, srv, st, "/") // creates admin "ben" + session "testtok"
	_ = rec0
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	st.CreateSession(t.Context(), "ben-elsewhere", ben.ID,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339))

	rec := authedPost(t, srv, st, "/settings/users/"+strconv.FormatInt(ben.ID, 10)+"/password",
		url.Values{"new_password": {"fresh-password"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok, _ := st.GetSession(t.Context(), "testtok"); !ok {
		t.Error("the session making the reset should survive")
	}
	if _, ok, _ := st.GetSession(t.Context(), "ben-elsewhere"); ok {
		t.Error("the admin's other sessions should be revoked")
	}
}

func TestPasswordResetIsAdminOnlyAndChecksTheUserExists(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	kimID := addUser(t, st, "kim", "old-password", "viewer", "kim-here")

	rec := postAs(t, srv, "kim-here", "/settings/users/"+strconv.FormatInt(kimID, 10)+"/password",
		url.Values{"new_password": {"kim-picks-this"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer reset: code=%d, want 403", rec.Code)
	}
	if !passwordWorks(t, st, "kim", "old-password") {
		t.Error("a refused reset must change nothing")
	}

	rec = authedPost(t, srv, st, "/settings/users/9999/password",
		url.Values{"new_password": {"nobody-home"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown user: code=%d, want 404", rec.Code)
	}
}

// The users tab has to offer both forms, or the feature is unreachable.
func TestUsersTabOffersPasswordForms(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/settings?tab=users").Body.String()
	for _, want := range []string{
		`action="/settings/password"`,
		`name="current_password"`,
		`/password"`,
		`name="new_password"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("users tab missing %q", want)
		}
	}
}
