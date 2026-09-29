package web

import (
	"context"
	"net/http"
	"strings"

	"netis/internal/notify"
	"netis/internal/web/views"
)

// notifyData reads the Notifications tab's settings. Credentials are reported
// only as present or not.
func (s *Server) notifyData(ctx context.Context) (views.NotifyData, error) {
	c, err := notify.LoadConfig(ctx, s.store)
	if err != nil {
		return views.NotifyData{}, err
	}
	d := views.NotifyData{
		BaseURL: c.BaseURL, WebhookURL: c.WebhookURL, NtfyURL: c.NtfyURL,
		HasWebhookAuth: c.WebhookAuth != "", HasNtfyToken: c.NtfyToken != "",
	}
	for _, g := range notify.Groups {
		d.Groups = append(d.Groups, views.NotifyGroup{Key: g.Key, Label: g.Label, Help: g.Help, On: !c.Off[g.Key]})
	}
	return d, nil
}

// handleNotificationsSave stores the Notifications tab. A blank credential
// keeps the stored one; its "clear" checkbox removes it. Every URL is checked
// before anything is written.
func (s *Server) handleNotificationsSave(w http.ResponseWriter, r *http.Request) {
	urls := map[string]string{}
	for _, k := range []string{notify.KeyBaseURL, notify.KeyWebhookURL, notify.KeyNtfyURL} {
		v := strings.TrimSpace(r.FormValue(k))
		if err := notify.ValidURL(v); err != nil {
			s.settingsError(w, r, "notifications", http.StatusBadRequest, err.Error())
			return
		}
		urls[k] = v
	}
	ctx := r.Context()
	for k, v := range urls {
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	for _, k := range []string{notify.KeyWebhookAuth, notify.KeyNtfyToken} {
		v := strings.TrimSpace(r.FormValue(k))
		if r.FormValue(k+"_clear") == "on" {
			v = ""
		} else if v == "" {
			continue
		}
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	for _, g := range notify.Groups {
		v := "0"
		if r.FormValue(g.Key) == "on" {
			v = "1"
		}
		if err := s.store.SetSetting(ctx, g.Key, v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/settings/notifications", http.StatusSeeOther)
}

// handleNotificationsTest sends a test message with the saved settings and
// toasts the outcome. Each channel is tried once, bounded by its timeout.
func (s *Server) handleNotificationsTest(w http.ResponseWriter, r *http.Request) {
	msg := "Test notification sent"
	if err := notify.SendTest(r.Context(), s.store); err != nil {
		msg = "Test notification failed: " + strings.ReplaceAll(err.Error(), "\n", "; ")
	}
	s.render(w, r, views.ScanToast(msg))
}
