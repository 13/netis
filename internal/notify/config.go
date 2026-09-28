// Package notify sends the events that matter on a home network — a new
// device, a watched device going offline, an IP conflict, an integration
// failing — to a webhook and/or an ntfy topic.
package notify

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"netis/internal/store"
)

// Setting keys. The two credentials are listed in store.SecretSettings, so
// they are encrypted at rest when NETIS_SECRET_KEY is set.
const (
	KeyBaseURL     = "notify_base_url"
	KeyWebhookURL  = "notify_webhook_url"
	KeyWebhookAuth = "notify_webhook_auth"
	KeyNtfyURL     = "notify_ntfy_url"
	KeyNtfyToken   = "notify_ntfy_token"
)

// Group is a set of event types switched on and off together, under the
// setting Key. Every group is on until it is switched off ("0").
type Group struct {
	Key, Label, Help string
	Types            []string
}

// Groups lists the notification toggles in the order the settings page shows
// them.
var Groups = []Group{
	{Key: "notify_device_new", Label: "New devices", Types: []string{"device_new"},
		Help: "A device netis has not seen before, from a scan or an integration."},
	{Key: "notify_offline", Label: "Offline / online", Types: []string{"offline", "online"},
		Help: "Only for devices marked \"alert when offline\" on their page."},
	{Key: "notify_ip_conflict", Label: "IP conflicts", Types: []string{"ip_conflict"},
		Help: "Two interfaces claiming the same address."},
	{Key: "notify_sync", Label: "Scan and integration errors", Types: []string{"scan_error", "sync_recovered"},
		Help: "A subnet sweep failing, an integration starting to fail, and its recovery."},
}

// groupOf maps each notifiable event type to its group's setting key. Types
// not listed here (ip_changed) are never sent.
var groupOf = func() map[string]string {
	m := map[string]string{}
	for _, g := range Groups {
		for _, t := range g.Types {
			m[t] = g.Key
		}
	}
	return m
}()

// Config is the notification settings as stored.
type Config struct {
	BaseURL     string
	WebhookURL  string
	WebhookAuth string
	NtfyURL     string
	NtfyToken   string
	// Off holds the group keys switched off.
	Off map[string]bool
}

// LoadConfig reads the notification settings. A credential that cannot be
// decrypted is an error rather than a blank value, as for the integrations.
func LoadConfig(ctx context.Context, st *store.Store) (Config, error) {
	get := func(k string) (string, error) {
		v, err := st.GetSetting(ctx, k)
		if err != nil {
			return "", fmt.Errorf("reading setting %s: %w", k, err)
		}
		return v, nil
	}
	var c Config
	var err error
	for k, dst := range map[string]*string{
		KeyBaseURL: &c.BaseURL, KeyWebhookURL: &c.WebhookURL, KeyWebhookAuth: &c.WebhookAuth,
		KeyNtfyURL: &c.NtfyURL, KeyNtfyToken: &c.NtfyToken,
	} {
		if *dst, err = get(k); err != nil {
			return Config{}, err
		}
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	c.Off = map[string]bool{}
	for _, g := range Groups {
		v, err := get(g.Key)
		if err != nil {
			return Config{}, err
		}
		if v == "0" {
			c.Off[g.Key] = true
		}
	}
	return c, nil
}

// HasChannel reports whether any channel is configured.
func (c Config) HasChannel() bool { return c.WebhookURL != "" || c.NtfyURL != "" }

// Wants reports whether events of typ are to be sent at all.
func (c Config) Wants(typ string) bool {
	key, ok := groupOf[typ]
	return ok && !c.Off[key]
}

// link returns the absolute URL of a page in netis, or "" without a base URL.
func (c Config) link(path string) string {
	if c.BaseURL == "" {
		return ""
	}
	return c.BaseURL + path
}

// ValidURL checks a channel or base URL typed into the settings form: blank
// (unset) or an absolute http(s) URL.
func ValidURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", s)
	}
	return nil
}
