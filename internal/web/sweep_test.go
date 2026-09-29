package web

import (
	"strings"
	"testing"

	"netis/internal/store"
)

// Tables left on settings pages carry the column name on every cell, so
// that on a phone they stack into labelled rows instead of scrolling.
func TestSettingsTablesStackOnPhones(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.AddAudit(t.Context(), store.AuditEntry{Username: "ben", Action: "login", Status: 303})
	for path, label := range map[string]string{
		"/settings/users":    `data-label="Role"`,
		"/settings/sessions": `data-label="Browser"`,
		"/settings/audit":    `data-label="Action"`,
	} {
		body := authedGet(t, srv, st, path).Body.String()
		if !strings.Contains(body, `class="table-stack"`) || !strings.Contains(body, label) {
			t.Errorf("%s: table does not stack on phones", path)
		}
	}
}

// The audit log names actions and outcomes in words.
func TestAuditShowsWords(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.AddAudit(t.Context(), store.AuditEntry{Action: "login", Detail: "wrong password", Status: 401})
	body := authedGet(t, srv, st, "/settings/audit").Body.String()
	for _, want := range []string{">Signed in<", ">Refused<", "Wrong password", "All actions"} {
		if !strings.Contains(body, want) {
			t.Errorf("audit page missing %q", want)
		}
	}
	for _, bad := range []string{">login<", ">401<", "→"} {
		if strings.Contains(body, bad) {
			t.Errorf("audit page shows %q", bad)
		}
	}
}

// The sidebar column runs the page's full height; only its content sticks.
func TestSidebarColumnHasStickyInner(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/").Body.String()
	if !strings.Contains(body, `<aside class="sidebar"><div class="sidebar-inner">`) {
		t.Error("sidebar has no sticky inner box")
	}
	css := authedGet(t, srv, st, "/static/app.css").Body.String()
	if !strings.Contains(css, ".sidebar-inner { position:sticky;") {
		t.Error("sidebar inner box is not sticky")
	}
}
