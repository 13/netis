package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
	"netis/internal/web/views"
)

// long is one character over the tag name limit; ok is exactly at it.
var (
	longTag = strings.Repeat("é", store.MaxTagName+1)
	okTag   = strings.Repeat("é", store.MaxTagName)
)

func tagCountTotal(t *testing.T, st *store.Store) int {
	t.Helper()
	tags, err := st.ListTags(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return len(tags)
}

// The device form refuses an over-long tag with the form error, keeping the
// form, and writes nothing.
func TestDeviceFormRefusesLongTag(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	before := deviceCount(t, st)
	rec := authedPost(t, srv, st, "/devices", url.Values{"name": {"nas"}, "kind": {"server"}, "tags": {"lab, " + longTag}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), views.Sentence(store.TagNameMsg)) {
		t.Fatalf("create: code=%d, body lacks %q", rec.Code, store.TagNameMsg)
	}
	if n := deviceCount(t, st); n != before {
		t.Errorf("refused create wrote a device: %d -> %d", before, n)
	}
	if n := tagCountTotal(t, st); n != 0 {
		t.Errorf("refused create left %d tags", n)
	}

	id, _ := st.CreateDevice(t.Context(), store.Device{Name: "box", Kind: "other", Source: "manual"})
	st.SetDeviceTags(t.Context(), id, []string{"keep"})
	rec = authedPost(t, srv, st, "/devices/"+itoa(id), url.Values{"name": {"renamed"}, "kind": {"other"}, "tags": {longTag}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), views.Sentence(store.TagNameMsg)) {
		t.Fatalf("update: code=%d, body lacks %q", rec.Code, store.TagNameMsg)
	}
	if d, _ := st.GetDevice(t.Context(), id); d.Name != "box" {
		t.Errorf("refused update renamed the device to %q", d.Name)
	}
	if tags, _ := st.DeviceTags(t.Context(), id); len(tags) != 1 || tags[0].Name != "keep" {
		t.Errorf("refused update changed tags: %+v", tags)
	}

	// Exactly at the limit is fine.
	rec = authedPost(t, srv, st, "/devices/"+itoa(id), url.Values{"name": {"box"}, "kind": {"other"}, "tags": {okTag}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("64-character tag: code=%d", rec.Code)
	}
}

func TestBulkTagRefusesLongTag(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	id, _ := st.CreateDevice(t.Context(), store.Device{Name: "box", Kind: "other", Source: "manual"})
	rec := authedPost(t, srv, st, "/devices/bulk/tag", url.Values{"id": {itoa(id)}, "tag": {"ok, " + longTag}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), store.TagNameMsg) {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if n := tagCountTotal(t, st); n != 0 {
		t.Errorf("refused bulk tag left %d tags", n)
	}
}

func TestAPIRefusesLongTag(t *testing.T) {
	srv, st, tok := adminToken(t)
	before := deviceCount(t, st)
	wantJSONError(t, bearer(t, srv, "POST", "/api/devices", tok,
		`{"name":"x","kind":"other","tags":["`+longTag+`"]}`), http.StatusBadRequest, store.TagNameMsg)
	if n := deviceCount(t, st); n != before {
		t.Errorf("refused create wrote a device: %d -> %d", before, n)
	}
	id, _ := st.CreateDevice(t.Context(), store.Device{Name: "box", Kind: "other", Source: "manual"})
	wantJSONError(t, bearer(t, srv, "PATCH", "/api/devices/"+itoa(id), tok,
		`{"name":"renamed","tags":["`+longTag+`"]}`), http.StatusBadRequest, store.TagNameMsg)
	if d, _ := st.GetDevice(t.Context(), id); d.Name != "box" {
		t.Errorf("refused patch renamed the device to %q", d.Name)
	}
	if n := tagCountTotal(t, st); n != 0 {
		t.Errorf("refused API writes left %d tags", n)
	}
}

func TestImportReportsLongTag(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	data := "MAC,Name,Tags\naa:00:00:00:00:01,box,ok;" + longTag + "\n"
	rec := importPost(t, srv, st, data, false)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "<strong>1</strong> with errors") || !strings.Contains(body, store.TagNameMsg) {
		t.Fatalf("preview: code=%d body lacks the tag error", rec.Code)
	}
	before := deviceCount(t, st)
	importPost(t, srv, st, data, true)
	if n := deviceCount(t, st); n != before {
		t.Errorf("commit wrote the bad row: %d -> %d", before, n)
	}
	if n := tagCountTotal(t, st); n != 0 {
		t.Errorf("commit left %d tags", n)
	}
}
