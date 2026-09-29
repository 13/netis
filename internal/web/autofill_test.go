package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

type fakeAutofill struct {
	runs  [][]int64
	kicks int
}

func (f *fakeAutofill) Run(_ context.Context, ids ...int64) error { f.runs = append(f.runs, ids); return nil }
func (f *fakeAutofill) Kick()                                     { f.kicks++ }

func TestAutofillSettingSavesAndKicks(t *testing.T) {
	srv, st := testServer(t)
	af := &fakeAutofill{}
	srv.autofill = af
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	form := url.Values{"section": {"network"}, "offline_after": {"3"}, "autofill_enabled": {"off"}}
	rec := authedPost(t, srv, st, "/settings/general", form)
	if rec.Code >= 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if v, _ := st.GetSetting(t.Context(), "autofill_enabled"); v != "off" {
		t.Fatalf("setting = %q", v)
	}
	if af.kicks != 1 {
		t.Fatalf("kicks = %d", af.kicks)
	}
	page := authedGet(t, srv, st, "/settings/network")
	if !strings.Contains(page.Body.String(), `name="autofill_enabled"`) {
		t.Fatal("toggle missing from the Network page")
	}
	bad := authedPost(t, srv, st, "/settings/general", url.Values{"section": {"network"}, "offline_after": {"3"}, "autofill_enabled": {"maybe"}})
	if bad.Code != 400 {
		t.Fatalf("bad value status = %d", bad.Code)
	}
}

func TestDeviceCreateKicksAutofill(t *testing.T) {
	srv, st := testServer(t)
	af := &fakeAutofill{}
	srv.autofill = af
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	rec := authedPost(t, srv, st, "/devices", url.Values{"name": {"x"}, "kind": {"other"}, "mac": {"3c:07:54:00:00:01"}})
	if rec.Code >= 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if af.kicks != 1 {
		t.Fatalf("kicks = %d", af.kicks)
	}
}
