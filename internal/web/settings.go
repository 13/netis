package web

import (
	"net/http"
	"strconv"
	"time"

	"netis/internal/buildinfo"
	"netis/internal/netdetect"
	"netis/internal/store"
	"netis/internal/web/views"
)

// adminSettings are the settings pages only an admin can open; the rest
// ("account", "sessions", "tokens") belong to every account.
var adminSettings = map[string]bool{
	"network": true, "integrations": true, "notifications": true, "tags": true,
	"users": true, "audit": true, "system": true,
}

// legacySettingsTabs maps the tabs of the old single settings page
// (/settings?tab=...) to the page that holds them now.
var legacySettingsTabs = map[string]string{
	"subnets": "network", "general": "network", "integrations": "integrations",
	"users": "users", "notifications": "notifications", "audit": "audit",
	"about": "system", "tokens": "tokens",
}

// handleSettingsPage sends /settings to a settings page: the account page,
// or for an old /settings?tab=... link the page that tab became. A viewer
// following a link to an admin tab lands on their account instead (the
// subnet list on the Subnets page), and the audit filters carry over.
func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tab := q.Get("tab")
	q.Del("tab")
	dest := "/settings/account"
	if page, ok := legacySettingsTabs[tab]; ok {
		switch {
		case !adminSettings[page] || isAdmin(r):
			dest = "/settings/" + page
		case tab == "subnets":
			dest = "/subnets"
		}
	}
	if len(q) > 0 && dest == "/settings/audit" {
		dest += "?" + q.Encode()
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// handleSettingsSection serves the settings page tab. Admin pages are
// registered behind requireAdmin as well; settingsData refuses them to a
// viewer regardless.
func (s *Server) handleSettingsSection(tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, err := s.settingsData(r, tab)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		u, _ := userFrom(r)
		s.render(w, r, views.SettingsPage(u.Username, d))
	}
}

// settingsError answers a settings form that could not be saved with the
// settings page open on tab and msg shown above it, under status. The page
// the form came from, rather than a bare text response the user has to go
// back from.
func (s *Server) settingsError(w http.ResponseWriter, r *http.Request, tab string, status int, msg string) {
	d, err := s.settingsData(r, tab)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d.Error = msg
	u, _ := userFrom(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, views.SettingsPage(u.Username, d))
}

// settingsWriteError answers a failed store write from a settings form: a
// conflict or missing reference inline on tab, anything else as a failure.
func (s *Server) settingsWriteError(w http.ResponseWriter, r *http.Request, tab string, err error, conflictMsg string) {
	if status, msg, ok := writeFailure(err, conflictMsg, ""); ok {
		s.settingsError(w, r, tab, status, msg)
		return
	}
	s.fail(w, r, err)
}

// settingsData gathers what the settings page shows on tab for the user
// making r.
func (s *Server) settingsData(r *http.Request, tab string) (views.SettingsData, error) {
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		return views.SettingsData{}, err
	}
	// Viewers get the page without the configuration behind it: no
	// integration addresses, users or key paths, no user list, and none of
	// the admin forms (the templates check the same flag).
	admin := isAdmin(r)
	var users []store.User
	if admin {
		users, err = s.store.ListUsers(r.Context())
		if err != nil {
			return views.SettingsData{}, err
		}
	}
	values := make(map[string]string)
	for _, k := range settingsKeys {
		if formSecrets[k] {
			// Never echo secrets back into the form.
			continue
		}
		v, err := s.store.GetSetting(r.Context(), k)
		if err != nil {
			return views.SettingsData{}, err
		}
		values[k] = v
	}
	offlineAfter, err := s.store.GetSetting(r.Context(), "offline_after")
	if err != nil {
		return views.SettingsData{}, err
	}
	values["offline_after"] = offlineAfter

	for _, k := range []string{"default_scan_interval_sec", "default_subnet_kind", "default_scan_enabled",
		"event_retention_days", "availability_retention_days", "audit_retention_days", "presence_fallback",
		"autofill_enabled"} {
		v, err := s.store.GetSetting(r.Context(), k)
		if err != nil {
			return views.SettingsData{}, err
		}
		values[k] = v
	}

	configured := configuredIntegrations(values)
	if !admin {
		values = map[string]string{}
	}

	switch {
	case tab == "account", tab == "sessions", tab == "tokens":
	case adminSettings[tab] && admin:
	default:
		tab = "account"
	}

	var newDetected []netdetect.Detected
	if detected, err := s.detect(); tab == "network" && err == nil {
		have := make(map[string]bool, len(subnets))
		for _, sn := range subnets {
			have[sn.CIDR] = true
		}
		for _, d := range detected {
			if !have[d.CIDR] {
				newDetected = append(newDetected, d)
			}
		}
	}

	u, _ := userFrom(r)
	var sessions []store.Session
	currentSessionID := ""
	var sso views.SSOAccount
	if tab == "sessions" {
		sessions, err = s.store.ListSessionsForUser(r.Context(), u.ID)
		if err != nil {
			return views.SettingsData{}, err
		}
		if c, cerr := r.Cookie("netis_session"); cerr == nil {
			currentSessionID = store.SessionID(c.Value)
		}
	}
	if tab == "account" {
		if s.oidc != nil {
			sso.Enabled = true
			if sso.Linked, err = s.store.OIDCLinked(r.Context(), u.ID); err != nil {
				return views.SettingsData{}, err
			}
		}
	}

	var tokens []store.APIToken
	if tab == "tokens" {
		// An admin manages everyone's tokens; anyone else sees their own.
		owner := u.ID
		if admin {
			owner = 0
		}
		if tokens, err = s.store.ListAPITokens(r.Context(), owner); err != nil {
			return views.SettingsData{}, err
		}
	}

	var statuses map[string]store.IntegrationStatus
	if tab == "integrations" {
		list, err := s.store.ListIntegrationStatus(r.Context())
		if err != nil {
			return views.SettingsData{}, err
		}
		statuses = make(map[string]store.IntegrationStatus, len(list))
		for _, it := range list {
			statuses[it.Name] = it
		}
	}

	var notifyData views.NotifyData
	if tab == "notifications" {
		if notifyData, err = s.notifyData(r.Context()); err != nil {
			return views.SettingsData{}, err
		}
	}

	var tags []store.TagCount
	if tab == "tags" {
		if tags, err = s.store.ListTagsWithCounts(r.Context()); err != nil {
			return views.SettingsData{}, err
		}
	}

	var audit views.AuditData
	if tab == "audit" {
		if audit, err = s.auditData(r); err != nil {
			return views.SettingsData{}, err
		}
	}

	me, _ := userFrom(r)
	return views.SettingsData{
		Notify:  notifyData,
		Audit:   audit,
		SSO:     sso,
		Tags:    tags,
		Subnets: subnets, Users: users, Values: values, Configured: configured,
		Sessions: sessions, CurrentSessionID: currentSessionID, CurrentUserID: me.ID,
		ActiveTab: tab, Detected: newDetected, Statuses: statuses, Tokens: tokens,
		About: views.AboutData{
			Info:    buildinfo.Get(),
			Uptime:  buildinfo.Uptime().String(),
			Backend: string(s.store.Dialect()),
			Now:     time.Now().UTC().Format(time.RFC3339),
			Backup:  s.backupSummary(),
		},
	}, nil
}

// backupSummary is the About tab's line on scheduled backups.
func (s *Server) backupSummary() views.BackupView {
	switch {
	case s.backups != nil:
		st := s.backups.Status()
		switch {
		case !st.LastAttempt.IsZero() && !st.OK:
			return views.BackupView{Failed: true, At: st.LastAttempt.UTC().Format(time.RFC3339)}
		case st.LastSuccess.IsZero():
			return views.BackupView{Note: "None yet"}
		}
		return views.BackupView{At: st.LastSuccess.UTC().Format(time.RFC3339), File: st.File}
	case s.store.Dialect() == store.Postgres:
		return views.BackupView{Note: "Not available on Postgres; back up with pg_dump"}
	}
	return views.BackupView{Note: "Off; set NETIS_BACKUP_DIR to turn it on"}
}

// retentionLabels name the retention settings the way the System page does.
var retentionLabels = map[string]string{
	"event_retention_days":        "event history",
	"availability_retention_days": "availability history",
	"audit_retention_days":        "audit log",
}

// badInput is a form value the caller got wrong, answered with a 400 and the
// message rather than as a server failure.
type badInput struct{ msg string }

func (e badInput) Error() string { return e.msg }

// handleGeneralSave saves the scanning settings from the Network page and
// the retention windows from the System page. Each page posts only its own
// fields and names itself in section; a field left out is left unchanged.
func (s *Server) handleGeneralSave(w http.ResponseWriter, r *http.Request) {
	tab := "network"
	if r.FormValue("section") == "system" {
		tab = "system"
	}
	if v := r.FormValue("offline_after"); v != "" || tab == "network" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10 {
			s.settingsError(w, r, tab, http.StatusBadRequest, "Offline after must be a whole number from 1 to 10")
			return
		}
		if err := s.store.SetSetting(r.Context(), "offline_after", strconv.Itoa(n)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("default_scan_interval_sec"); v != "" {
		iv, err := strconv.Atoi(v)
		if err != nil || iv < 30 {
			s.settingsError(w, r, tab, http.StatusBadRequest, "the scan interval must be a whole number of seconds, 30 or more")
			return
		}
		if err := s.store.SetSetting(r.Context(), "default_scan_interval_sec", strconv.Itoa(iv)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("default_subnet_kind"); v != "" {
		if v != "lan" && v != "wireguard" && v != "proxmox-bridge" {
			s.settingsError(w, r, tab, http.StatusBadRequest, "choose a subnet kind from the list")
			return
		}
		if err := s.store.SetSetting(r.Context(), "default_subnet_kind", v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("default_scan_enabled"); v != "" {
		if v != "on" && v != "off" {
			s.settingsError(w, r, tab, http.StatusBadRequest, "choose whether new subnets are scanned: On or Off")
			return
		}
		if err := s.store.SetSetting(r.Context(), "default_scan_enabled", v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("presence_fallback"); v != "" {
		if v != "on" && v != "off" {
			s.settingsError(w, r, tab, http.StatusBadRequest, "choose a presence fallback from the list")
			return
		}
		if err := s.store.SetSetting(r.Context(), "presence_fallback", v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("autofill_enabled"); v != "" {
		if v != "on" && v != "off" {
			s.settingsError(w, r, tab, http.StatusBadRequest, "choose whether netis fills in device details: On or Off")
			return
		}
		if err := s.store.SetSetting(r.Context(), "autofill_enabled", v); err != nil {
			s.fail(w, r, err)
			return
		}
		s.kickAutofill()
	}
	// Retention windows, in days. Zero is meaningful — keep forever — so it is
	// accepted rather than treated as unset.
	for _, k := range []string{"event_retention_days", "availability_retention_days", "audit_retention_days"} {
		v := r.FormValue(k)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.settingsError(w, r, tab, http.StatusBadRequest, retentionLabels[k]+" must be a whole number of days, 0 or more")
			return
		}
		if err := s.store.SetSetting(r.Context(), k, strconv.Itoa(n)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/settings/"+tab, http.StatusSeeOther)
}
