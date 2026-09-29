package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/buildinfo"
	"netis/internal/netdetect"
	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
	"netis/internal/wireguard"
)

// settingsKeys is the fixed set of settings written by the integrations
// form. The secrets among them (formSecrets) are handled specially: they're
// never echoed back into the form, and posting a blank value keeps the
// existing stored secret. The *_insecure keys are checkboxes.
var settingsKeys = []string{
	"proxmox_url", "proxmox_token_id", "proxmox_secret", "proxmox_insecure",
	"wg_ssh_addr", "wg_ssh_user", "wg_ssh_key_path", "wg_ssh_known_hosts", "wg_iface",
	"pihole_url", "pihole_password", "pihole_insecure",
	"adguard_url", "adguard_user", "adguard_password", "adguard_insecure",
	"opnsense_url", "opnsense_key", "opnsense_secret", "opnsense_insecure",
}

// formSecrets are the integrations form's credential fields.
var formSecrets = map[string]bool{
	"proxmox_secret": true, "pihole_password": true, "adguard_password": true, "opnsense_secret": true,
}

// adminSettings are the settings pages only an admin can open; the rest
// ("account", "sessions", "tokens") belong to every account.
var adminSettings = map[string]bool{
	"network": true, "integrations": true, "notifications": true,
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
		"event_retention_days", "availability_retention_days", "audit_retention_days", "presence_fallback"} {
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

func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	sn, msg := parseSubnetForm(r)
	if msg != "" {
		s.settingsError(w, r, "network", http.StatusBadRequest, msg)
		return
	}
	auditNote(r).Target = "subnet " + sn.CIDR
	if _, err := s.store.CreateSubnet(r.Context(), sn); err != nil {
		s.settingsWriteError(w, r, "network", err, subnetExistsMsg)
		return
	}
	http.Redirect(w, r, "/settings/network", http.StatusSeeOther)
}

func (s *Server) handleSubnetUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetSubnet(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	sn, msg := parseSubnetForm(r)
	if msg != "" {
		s.settingsError(w, r, "network", http.StatusBadRequest, msg)
		return
	}
	sn.ID = id
	auditNote(r).Target = "subnet " + sn.CIDR
	if err := s.store.UpdateSubnet(r.Context(), sn); err != nil {
		s.settingsWriteError(w, r, "network", err, subnetExistsMsg)
		return
	}
	http.Redirect(w, r, "/settings/network", http.StatusSeeOther)
}

const subnetExistsMsg = "a subnet with that CIDR already exists"

// parseSubnetForm validates and builds a store.Subnet from the request form,
// returning what is wrong with it as msg on validation failure. The
// CIDR is normalized to its masked form (e.g. "10.0.0.5/24" -> "10.0.0.0/24")
// so stored subnets are always canonical regardless of what a user typed.
// retentionLabels name the retention settings the way the System page does.
var retentionLabels = map[string]string{
	"event_retention_days":        "event history",
	"availability_retention_days": "availability history",
	"audit_retention_days":        "audit log",
}

func parseSubnetForm(r *http.Request) (sn store.Subnet, msg string) {
	cidr := strings.TrimSpace(r.FormValue("cidr"))
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return store.Subnet{}, "that is not a subnet in CIDR form; write it like 192.168.1.0/24"
	}
	// Every address in a subnet becomes a grid cell and a sweep target, so an
	// over-wide prefix is refused here rather than discovered when the page is
	// opened. The message names the limit and the prefix that would fit.
	if err := scan.CheckSubnetSize(cidr); err != nil {
		return store.Subnet{}, err.Error()
	}
	kind := r.FormValue("kind")
	if kind != "lan" && kind != "wireguard" && kind != "proxmox-bridge" {
		return store.Subnet{}, "choose a subnet kind from the list"
	}
	interval, err := strconv.Atoi(r.FormValue("scan_interval_sec"))
	if err != nil || interval < 30 {
		interval = 120
	}
	return store.Subnet{
		CIDR: prefix.Masked().String(), Name: r.FormValue("name"), Kind: kind,
		ScanEnabled: r.FormValue("scan_enabled") == "on", ScanIntervalSec: interval,
	}, ""
}

func (s *Server) handleSubnetDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sn, err := s.store.GetSubnet(r.Context(), id); err == nil {
		auditNote(r).Target = "subnet " + sn.CIDR
	}
	if err := s.store.DeleteSubnet(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/network", http.StatusSeeOther)
}

// integrationKeys are the settings keys of each integration, for a form that
// saves one integration: its panel on the settings page posts only its own
// fields, and the others must be left as they are.
var integrationKeys = func() map[string][]string {
	prefixes := map[string]string{
		"proxmox": "proxmox_", "wireguard": "wg_", "pihole": "pihole_",
		"adguard": "adguard_", "opnsense": "opnsense_",
	}
	m := make(map[string][]string, len(prefixes))
	for name, prefix := range prefixes {
		for _, k := range settingsKeys {
			if strings.HasPrefix(k, prefix) {
				m[name] = append(m[name], k)
			}
		}
	}
	return m
}()

// saveIntegrationSettings writes the integration settings from a submitted
// form: a blank secret keeps the stored value, and the *_insecure checkboxes
// normalize "on" to "1". A form naming one integration (the settings page's
// per-integration panels) writes only that integration's keys; one naming
// none (the setup wizard) writes them all. A value that fails validation is
// returned as a badInput, before anything from the form is written.
func (s *Server) saveIntegrationSettings(r *http.Request) error {
	keys := settingsKeys
	if name := r.FormValue("integration"); name != "" {
		if keys = integrationKeys[name]; keys == nil {
			return badInput{"unknown integration " + strconv.Quote(name)}
		}
	}
	// The interface name is spliced into a command the WireGuard host's shell
	// runs. Blank is fine: it falls back to wg0.
	if v := r.FormValue("wg_iface"); v != "" {
		if err := wireguard.ValidIface(v); err != nil {
			return badInput{err.Error()}
		}
	}
	for _, k := range keys {
		v := r.FormValue(k)
		if formSecrets[k] && v == "" {
			// Blank means "leave unchanged" — don't wipe the stored secret.
			continue
		}
		if strings.HasSuffix(k, "_insecure") {
			// Normalize the checkbox ("on"/"") to the "1"/"" that main.go reads.
			if v == "on" {
				v = "1"
			} else {
				v = ""
			}
		}
		if err := s.store.SetSetting(r.Context(), k, v); err != nil {
			return err
		}
	}
	return nil
}

// badInput is a form value the caller got wrong, answered with a 400 and the
// message rather than as a server failure.
type badInput struct{ msg string }

func (e badInput) Error() string { return e.msg }

func (s *Server) handleIntegrationsSave(w http.ResponseWriter, r *http.Request) {
	if err := s.saveIntegrationSettings(r); err != nil {
		var bad badInput
		if errors.As(err, &bad) {
			s.settingsError(w, r, "integrations", http.StatusBadRequest, bad.msg)
			return
		}
		s.fail(w, r, err)
		return
	}
	dest := "/settings/integrations"
	if name := r.FormValue("integration"); integrationKeys[name] != nil {
		dest += "#integration-" + name
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	role := r.FormValue("role")
	note := auditNote(r)
	note.Target, note.Detail = "user "+username, "role "+role
	if username == "" || len(password) < minPasswordLen {
		s.settingsError(w, r, "users", http.StatusBadRequest, "enter a username and a password of at least "+strconv.Itoa(minPasswordLen)+" characters")
		return
	}
	if len(password) > maxPasswordLen {
		s.settingsError(w, r, "users", http.StatusBadRequest, passwordTooLongMsg)
		return
	}
	if role != "admin" && role != "viewer" {
		s.settingsError(w, r, "users", http.StatusBadRequest, "choose a role: Admin or Viewer")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.CreateUser(r.Context(), username, string(hash), role); err != nil {
		s.settingsWriteError(w, r, "users", err, "a user named "+username+" already exists")
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusSeeOther)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u, found, err := s.store.GetUser(r.Context(), id); err == nil && found {
		auditNote(r).Target = "user " + u.Username
	}
	// Deleting yourself would sign you out mid-click and, for the last
	// admin with SSO off, lock everyone out; another admin has to do it.
	if me, ok := userFrom(r); ok && me.ID == id {
		s.settingsError(w, r, "users", http.StatusBadRequest, "you cannot delete your own account; sign in as another admin to delete it")
		return
	}
	// DeleteUserGuarded performs the existence check, admin count, and
	// delete atomically in a single SQL statement so two concurrent
	// deletes of two different admins can't both succeed and leave zero
	// admins (a TOCTOU race a separate ListUsers-then-DeleteUser sequence
	// would be vulnerable to).
	deleted, err := s.store.DeleteUserGuarded(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !deleted {
		// Distinguish "no such user" (404) from "refused: last admin"
		// (400) for a useful error response.
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		exists := false
		for _, u := range users {
			if u.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
		s.settingsError(w, r, "users", http.StatusBadRequest, "this is the only admin; make another user an admin first, then delete this one")
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusSeeOther)
}

// handleUserRole promotes a viewer or demotes an admin. Sessions are left
// alone: the role is read from the database on every request, so the change
// applies to the user's very next one.
func (s *Server) handleUserRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u, found, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	role := r.FormValue("role")
	note := auditNote(r)
	note.Target, note.Detail = "user "+u.Username, u.Role+" -> "+role
	if role != "admin" && role != "viewer" {
		note.Detail = "choose a role: Admin or Viewer"
		s.settingsError(w, r, "users", http.StatusBadRequest, "choose a role: Admin or Viewer")
		return
	}
	// Guarded in one statement, like delete: two admins demoting each other
	// at once cannot leave nobody able to administer netis.
	changed, err := s.store.SetUserRoleGuarded(r.Context(), id, role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !changed {
		s.settingsError(w, r, "users", http.StatusBadRequest, "this is the only admin; make another user an admin first, then change this role")
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusSeeOther)
}

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
