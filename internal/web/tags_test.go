package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

// seedTags gives two devices the tags nas (both) and media (one), and
// returns the tag ids by name.
func seedTags(t *testing.T, st *store.Store) map[string]int64 {
	t.Helper()
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	a, _ := st.CreateDevice(ctx, store.Device{Name: "a", Kind: "other", Source: "manual"})
	b, _ := st.CreateDevice(ctx, store.Device{Name: "b", Kind: "other", Source: "manual"})
	if err := st.SetDeviceTags(ctx, a, []string{"nas", "media"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDeviceTags(ctx, b, []string{"nas"}); err != nil {
		t.Fatal(err)
	}
	tags, err := st.ListTagsWithCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, tc := range tags {
		ids[tc.Name] = tc.ID
	}
	return ids
}

func tagCount(t *testing.T, st *store.Store, name string) (store.TagCount, bool) {
	t.Helper()
	tags, err := st.ListTagsWithCounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tags {
		if tc.Name == name {
			return tc, true
		}
	}
	return store.TagCount{}, false
}

func toastOf(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == toastCookie {
			v, _ := url.QueryUnescape(c.Value)
			return v
		}
	}
	return ""
}

func TestTagsSettingsPageListsTags(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	rec := authedGet(t, srv, st, "/settings/tags")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`href="/settings/tags" aria-current="page"`,
		`class="tag tag-`,
		`href="/devices?tag=nas"`, ">2 devices<",
		`href="/devices?tag=media"`, ">1 device<",
		fmt.Sprintf(`hx-post="/settings/tags/%d/color"`, ids["nas"]),
		fmt.Sprintf(`action="/settings/tags/%d/rename"`, ids["nas"]),
		fmt.Sprintf(`action="/settings/tags/%d/delete"`, ids["media"]),
		`data-confirm="Remove media from 1 device?"`,
		`data-confirm="Remove nas from 2 devices?"`,
		`class="table-stack`,
		`<noscript>`,
		`aria-label="Violet"`,
		`aria-label="Auto"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestTagsSettingsEmptyState(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/settings/tags").Body.String()
	if !strings.Contains(body, "No tags yet. Add tags to devices from their edit form or the device list.") {
		t.Error("empty state missing")
	}
}

func TestTagsSettingsAdminOnly(t *testing.T) {
	srv, st := testServer(t)
	seedTags(t, st)
	cookie := viewerSession(t, st)
	req := httptest.NewRequest("GET", "/settings/tags", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer GET = %d, want 403", rec.Code)
	}
	if body := viewerGet(t, srv, st, "/settings/account"); strings.Contains(body, `href="/settings/tags"`) {
		t.Error("viewer nav links to the tags page")
	}
}

func TestTagColorSave(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	path := fmt.Sprintf("/settings/tags/%d/color", ids["nas"])

	rec := htmxRequest(t, srv, st, "POST", path, url.Values{"color": {"violet"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Colour saved") {
		t.Fatalf("htmx colour = %d: %s", rec.Code, rec.Body.String())
	}
	if tc, _ := tagCount(t, st, "nas"); tc.Color != "violet" {
		t.Fatalf("colour = %q", tc.Color)
	}
	if !strings.Contains(rec.Body.String(), "tag-violet") {
		t.Error("htmx response should redraw the chip in its new colour")
	}

	rec = authedPost(t, srv, st, path, url.Values{"color": {""}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/tags" {
		t.Fatalf("plain colour = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if toastOf(rec) != "Colour saved" {
		t.Errorf("toast = %q", toastOf(rec))
	}
	if tc, _ := tagCount(t, st, "nas"); tc.Color != "" {
		t.Fatalf("colour = %q, want auto", tc.Color)
	}

	es := auditEntries(t, st)
	if len(es) == 0 || es[0].Action != "tag.color" {
		t.Errorf("audit = %+v", es)
	}
}

func TestTagColorRejectsUnknownKey(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	path := fmt.Sprintf("/settings/tags/%d/color", ids["nas"])
	for _, c := range []string{"#ff0000", "red", "Teal"} {
		rec := authedPost(t, srv, st, path, url.Values{"color": {c}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Choose a colour from the list") {
			t.Errorf("colour %q = %d", c, rec.Code)
		}
		rec = htmxRequest(t, srv, st, "POST", path, url.Values{"color": {c}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "choose a colour from the list") {
			t.Errorf("htmx colour %q = %d: %s", c, rec.Code, rec.Body.String())
		}
	}
	if tc, _ := tagCount(t, st, "nas"); tc.Color != "" {
		t.Errorf("colour changed to %q", tc.Color)
	}
	if rec := authedPost(t, srv, st, "/settings/tags/9999/color", url.Values{"color": {"teal"}}); rec.Code != http.StatusNotFound {
		t.Errorf("missing tag = %d", rec.Code)
	}
}

func TestTagRename(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	rec := authedPost(t, srv, st, fmt.Sprintf("/settings/tags/%d/rename", ids["media"]), url.Values{"name": {"  video  "}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/tags" {
		t.Fatalf("rename = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if toastOf(rec) != "Renamed to video" {
		t.Errorf("toast = %q", toastOf(rec))
	}
	if tc, ok := tagCount(t, st, "video"); !ok || tc.ID != ids["media"] || tc.Devices != 1 {
		t.Fatalf("video = %+v ok=%v", tc, ok)
	}
	es := auditEntries(t, st)
	if len(es) == 0 || es[0].Action != "tag.rename" || es[0].Target != "tag media" {
		t.Errorf("audit = %+v", es[0])
	}
}

func TestTagRenameMerges(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	st.SetTagColor(t.Context(), ids["nas"], "teal")
	path := fmt.Sprintf("/settings/tags/%d/rename", ids["media"])

	// Without confirm the merge waits: the page asks first.
	rec := authedPost(t, srv, st, path, url.Values{"name": {"nas"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("unconfirmed rename = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"nas already exists; its 2 devices and this 1 will share it.",
		`name="confirm" value="1"`,
		`<input type="hidden" name="name" value="nas"`,
		">Merge<",
		`href="/settings/tags"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation missing %q", want)
		}
	}
	if _, ok := tagCount(t, st, "media"); !ok {
		t.Fatal("media merged without confirmation")
	}
	if tc, _ := tagCount(t, st, "nas"); tc.Devices != 2 {
		t.Fatalf("nas = %+v", tc)
	}

	rec = authedPost(t, srv, st, path, url.Values{"name": {"nas"}, "confirm": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rename = %d: %s", rec.Code, rec.Body.String())
	}
	if toastOf(rec) != "Merged into nas" {
		t.Errorf("toast = %q", toastOf(rec))
	}
	if _, ok := tagCount(t, st, "media"); ok {
		t.Error("media still exists")
	}
	if tc, _ := tagCount(t, st, "nas"); tc.Devices != 2 || tc.Color != "teal" {
		t.Errorf("nas = %+v", tc)
	}
	es := auditEntries(t, st)
	if len(es) == 0 || es[0].Detail != "media -> nas (merged)" {
		t.Errorf("audit = %+v", es[0])
	}
}

func TestTagRenameMergeConfirmCounts(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	rec := authedPost(t, srv, st, fmt.Sprintf("/settings/tags/%d/rename", ids["nas"]), url.Values{"name": {"media"}})
	if !strings.Contains(rec.Body.String(), "media already exists; its 1 device and these 2 will share it.") {
		t.Errorf("confirmation text wrong: %s", rec.Body.String())
	}
	if _, ok := tagCount(t, st, "nas"); !ok {
		t.Fatal("nas merged without confirmation")
	}
}

// A unique violation from the rename (the name was created meanwhile) is a
// conflict shown on the page, not a server failure.
func TestTagRenameRaceIsConflict(t *testing.T) {
	srv, st := testServer(t)
	seedTags(t, st)
	_, err := st.CreateTag(t.Context(), "nas", "")
	if !store.IsUniqueViolation(err) {
		t.Fatalf("expected a unique violation, got %v", err)
	}
	authedGet(t, srv, st, "/") // admin exists
	req := httptest.NewRequest("POST", "/settings/tags/1/rename", nil)
	rec := httptest.NewRecorder()
	srv.tagWriteError(rec, req, err)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "A tag with that name was just created; try again") {
		t.Errorf("race = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTagSettingsInputs(t *testing.T) {
	srv, st := testServer(t)
	seedTags(t, st)
	body := authedGet(t, srv, st, "/settings/tags").Body.String()
	if !strings.Contains(body, `hx-trigger="change delay:600ms"`) {
		t.Error("swatch picker should debounce keyboard changes")
	}
	if strings.Contains(body, `maxlength="64"`) {
		t.Error("rename input keeps a UTF-16 maxlength")
	}
}

func TestTagRenameRejectsBadNames(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	path := fmt.Sprintf("/settings/tags/%d/rename", ids["media"])
	for _, n := range []string{"", "   ", strings.Repeat("x", 65)} {
		rec := authedPost(t, srv, st, path, url.Values{"name": {n}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "A tag name is 1 to 64 characters") {
			t.Errorf("name %q = %d", n, rec.Code)
		}
	}
	// 64 characters, some multi-byte, is fine.
	ok := strings.Repeat("é", 64)
	if rec := authedPost(t, srv, st, path, url.Values{"name": {ok}}); rec.Code != http.StatusSeeOther {
		t.Errorf("64-rune name = %d", rec.Code)
	}
	if rec := authedPost(t, srv, st, "/settings/tags/9999/rename", url.Values{"name": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("missing tag = %d", rec.Code)
	}
}

func TestTagDelete(t *testing.T) {
	srv, st := testServer(t)
	ids := seedTags(t, st)
	rec := authedPost(t, srv, st, fmt.Sprintf("/settings/tags/%d/delete", ids["nas"]), url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/tags" {
		t.Fatalf("delete = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if toastOf(rec) != "Tag deleted" {
		t.Errorf("toast = %q", toastOf(rec))
	}
	if _, ok := tagCount(t, st, "nas"); ok {
		t.Error("nas still exists")
	}
	es := auditEntries(t, st)
	if len(es) == 0 || es[0].Action != "tag.delete" || es[0].Target != "tag nas" {
		t.Errorf("audit = %+v", es[0])
	}
	if rec := authedPost(t, srv, st, fmt.Sprintf("/settings/tags/%d/delete", ids["nas"]), url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d", rec.Code)
	}
}
