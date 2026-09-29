package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/notify"
)

func TestNotificationsTabIsAdminOnly(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.SetSetting(t.Context(), notify.KeyWebhookURL, "https://hooks.internal/netis")

	admin := authedGet(t, srv, st, "/settings?tab=notifications").Body.String()
	for _, want := range []string{`action="/settings/notifications"`, "https://hooks.internal/netis",
		`hx-post="/settings/notifications/test"`, `name="notify_offline"`} {
		if !strings.Contains(admin, want) {
			t.Errorf("admin notifications tab missing %q", want)
		}
	}

	viewer := viewerGet(t, srv, st, "/settings?tab=notifications")
	for _, leak := range []string{"tab=notifications", "hooks.internal", "/settings/notifications"} {
		if strings.Contains(viewer, leak) {
			t.Errorf("viewer settings page contains %q", leak)
		}
	}
}

func TestNotificationsSave(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	get := func(k string) string {
		v, err := st.GetSetting(t.Context(), k)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	wantRedirect(t, authedPost(t, srv, st, "/settings/notifications", url.Values{
		"notify_webhook_url":  {" https://hooks.lan/x "},
		"notify_webhook_auth": {"Bearer abc"},
		"notify_ntfy_url":     {"https://ntfy.sh/netis"},
		"notify_ntfy_token":   {"tk"},
		"notify_base_url":     {"https://netis.lan"},
		"notify_device_new":   {"on"},
		"notify_ip_conflict":  {"on"},
	}), "/settings?tab=notifications")
	if get(notify.KeyWebhookURL) != "https://hooks.lan/x" || get(notify.KeyWebhookAuth) != "Bearer abc" ||
		get(notify.KeyNtfyToken) != "tk" || get("notify_device_new") != "1" || get("notify_offline") != "0" {
		t.Fatal("settings not stored as submitted")
	}

	// The credentials are never echoed back into the page.
	body := authedGet(t, srv, st, "/settings?tab=notifications").Body.String()
	if strings.Contains(body, "Bearer abc") || strings.Contains(body, `value="tk"`) {
		t.Fatal("stored credential rendered into the form")
	}

	// Blank keeps a credential; clear removes it.
	authedPost(t, srv, st, "/settings/notifications", url.Values{
		"notify_webhook_url": {"https://hooks.lan/x"}, "notify_ntfy_token_clear": {"on"},
	})
	if get(notify.KeyWebhookAuth) != "Bearer abc" || get(notify.KeyNtfyToken) != "" {
		t.Fatalf("auth=%q token=%q, want kept and cleared", get(notify.KeyWebhookAuth), get(notify.KeyNtfyToken))
	}

	// A bad URL is refused on the tab, and nothing is written.
	rec := authedPost(t, srv, st, "/settings/notifications", url.Values{
		"notify_webhook_url": {"https://other.lan"}, "notify_ntfy_url": {"ntfy.sh/topic"},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not an http(s) URL") {
		t.Fatalf("bad url: code=%d", rec.Code)
	}
	if get(notify.KeyWebhookURL) != "https://hooks.lan/x" {
		t.Fatal("refused form still wrote the webhook URL")
	}
}

func TestNotificationsSendTest(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	if body := authedPost(t, srv, st, "/settings/notifications/test", nil).Body.String(); !strings.Contains(body, "no channel configured") {
		t.Fatalf("unconfigured test toast = %q", body)
	}

	got := make(chan string, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- string(b)
	}))
	defer hook.Close()
	st.SetSetting(t.Context(), notify.KeyWebhookURL, hook.URL)
	if body := authedPost(t, srv, st, "/settings/notifications/test", nil).Body.String(); !strings.Contains(body, "Test notification sent") {
		t.Fatalf("test toast = %q", body)
	}
	if b := <-got; !strings.Contains(b, `"event":"test"`) {
		t.Fatalf("webhook body = %s", b)
	}
}

func TestDeviceAlertToggle(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := seedInventory(t, st)
	path := fmt.Sprintf("/devices/%d", devID)

	if body := authedGet(t, srv, st, path).Body.String(); !strings.Contains(body, "Alert when offline") {
		t.Fatal("device page has no alert toggle")
	}
	wantRedirect(t, authedPost(t, srv, st, path+"/alert", url.Values{"alert_offline": {"1"}}), path)
	if _, on, _ := st.DeviceAlert(t.Context(), devID); !on {
		t.Fatal("flag not set")
	}
	body := authedGet(t, srv, st, path).Body.String()
	if !strings.Contains(body, "offline alerts on") || !strings.Contains(body, "Stop offline alerts") {
		t.Fatal("device page does not show the flag as on")
	}
	// Viewers see the state but not the button.
	if v := viewerGet(t, srv, st, path); !strings.Contains(v, "offline alerts on") || strings.Contains(v, "/alert") {
		t.Fatal("viewer page: state missing or toggle shown")
	}
	authedPost(t, srv, st, path+"/alert", url.Values{"alert_offline": {"0"}})
	if _, on, _ := st.DeviceAlert(t.Context(), devID); on {
		t.Fatal("flag not cleared")
	}
	if rec := authedPost(t, srv, st, "/devices/9999/alert", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing device: code=%d, want 404", rec.Code)
	}
}

// The stored-credential state renders as markup, not as the templ call that
// produces it.
func TestNotificationsTabRendersSecretState(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.SetSetting(t.Context(), notify.KeyNtfyToken, "tk")

	body := authedGet(t, srv, st, "/settings?tab=notifications").Body.String()
	if strings.Contains(body, "@secretState") {
		t.Fatal("raw templ call rendered into the page")
	}
	if !strings.Contains(body, `Stored header: <span class="muted">none</span>`) || !strings.Contains(body, `Stored token: <span class="badge">stored</span>`) {
		t.Fatal("stored-credential state missing")
	}
}
