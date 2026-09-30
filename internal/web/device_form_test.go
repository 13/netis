package web

import (
	"strings"
	"testing"

	"netis/internal/store"
	"netis/internal/web/views"
)

// The edit form offers known values as suggestions and, where autofill
// detected something else than the device holds, a button to use it.
func TestDeviceFormSuggestsDetected(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	id, _ := st.CreateDevice(ctx, store.Device{Name: "printer", Kind: "other", Vendor: "Mine", Source: "manual"})
	st.ReplaceHints(ctx, id, "hostname", []store.Hint{
		{Field: "vendor", Value: "Brother", Confidence: 80, Detail: "hostname BRW1", SeenAt: "2026-09-29T00:00:00Z"},
		{Field: "model", Value: "HL-L2350", Confidence: 80, Detail: "hostname BRW1", SeenAt: "2026-09-29T00:00:00Z"},
		{Field: "tag", Value: "nas", Confidence: 60, Detail: "hostname BRW1", SeenAt: "2026-09-29T00:00:00Z"},
	})
	body := authedGet(t, srv, st, "/devices/"+itoa(id)+"/edit").Body.String()
	for _, want := range []string{
		`id="dl-vendor"`, `id="dl-model"`, `id="dl-function"`, `list="dl-vendor"`,
		`<option value="Mine">`, `<option value="Brother">`, `<option value="HL-L2350">`,
		`data-fill="vendor"`, `data-value="Brother"`, "Use detected: Brother",
		`data-fill="model"`, "Use detected: HL-L2350",
		`data-append-tag="nas"`, "Add detected tag: nas",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form lacks %q", want)
		}
	}
	if strings.Contains(body, `data-fill="function"`) {
		t.Error("offers a function nothing detected")
	}

	body = authedGet(t, srv, st, "/devices/new").Body.String()
	if !strings.Contains(body, `id="dl-vendor"`) || !strings.Contains(body, `<option value="Mine">`) {
		t.Error("new form lacks the vendor suggestions")
	}
	if strings.Contains(body, "Use detected") || strings.Contains(body, "Add detected tag") {
		t.Error("new form offers detected values")
	}
}

// A detected value the device already holds, or a tag it already has, is
// not offered again.
func TestDeviceFormHidesDetectedItHas(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	id, _ := st.CreateDevice(ctx, store.Device{Name: "p", Kind: "printer", Vendor: "Brother", Source: "manual"})
	st.SetDeviceTags(ctx, id, []string{"nas"})
	st.ReplaceHints(ctx, id, "hostname", []store.Hint{
		{Field: "vendor", Value: "Brother", Confidence: 80, SeenAt: "2026-09-29T00:00:00Z"},
		{Field: "kind", Value: "printer", Confidence: 80, SeenAt: "2026-09-29T00:00:00Z"},
		{Field: "tag", Value: "nas", Confidence: 60, SeenAt: "2026-09-29T00:00:00Z"},
	})
	body := authedGet(t, srv, st, "/devices/"+itoa(id)+"/edit").Body.String()
	if strings.Contains(body, "Use detected") || strings.Contains(body, "Add detected tag") {
		t.Error("offers values the device already has")
	}
}

// The tags input carries what the chip editor (static/tags.js) needs: the
// marker, and every existing tag with its colour already resolved.
func TestDeviceFormTagEditorData(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	id, _ := st.CreateDevice(ctx, store.Device{Name: "p", Kind: "other", Source: "manual"})
	st.SetDeviceTags(ctx, id, []string{"nas", "media"})
	if _, err := st.DB.Exec(`UPDATE tag SET color = 'violet' WHERE name = 'nas'`); err != nil {
		t.Fatal(err)
	}
	body := authedGet(t, srv, st, "/devices/"+itoa(id)+"/edit").Body.String()
	for _, want := range []string{
		`data-tag-editor`,
		`{&#34;name&#34;:&#34;nas&#34;,&#34;color&#34;:&#34;violet&#34;}`,
		`{&#34;name&#34;:&#34;media&#34;,&#34;color&#34;:&#34;` + views.TagColor("media", "") + `&#34;}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form lacks %q", want)
		}
	}
}
