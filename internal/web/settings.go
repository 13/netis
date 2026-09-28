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
// form. proxmox_secret and pihole_password are handled specially: they're
// never echoed back into the form, and posting a blank value keeps the
// existing stored secret.
var settingsKeys = []string{
	"proxmox_url", "proxmox_token_id", "proxmox_secret", "proxmox_insecure",
	"wg_ssh_addr", "wg_ssh_user", "wg_ssh_key_path", "wg_ssh_known_hosts", "wg_iface",
	"pihole_url", "pihole_password", "pihole_insecure",
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	d, err := s.settingsData(r, r.URL.Query().Get("tab"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, _ := userFrom(r)
	s.render(w, r, views.SettingsPage(u.Username, d))
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
		if k == "proxmox_secret" || k == "pihole_password" {
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
		"event_retention_days", "availability_retention_days", "presence_fallback"} {
		v, err := s.store.GetSetting(r.Context(), k)
		if err != nil {
			return views.SettingsData{}, err
		}
		values[k] = v
	}

	configured := map[string]bool{
		"proxmox":   values["proxmox_url"] != "",
		"wireguard": values["wg_ssh_addr"] != "",
		"pihole":    values["pihole_url"] != "",
	}
	if !admin {
		values = map[string]string{}
	}

	switch tab {
	case "subnets", "integrations", "users", "tokens", "about":
	case "general", "notifications":
		if !admin {
			tab = "subnets"
		}
	default:
		tab = "subnets"
	}

	var newDetected []netdetect.Detected
	if detected, err := s.detect(); admin && err == nil {
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
	if tab == "users" {
		sessions, err = s.store.ListSessionsForUser(r.Context(), u.ID)
		if err != nil {
			return views.SettingsData{}, err
		}
		if c, cerr := r.Cookie("netis_session"); cerr == nil {
			currentSessionID = store.SessionID(c.Value)
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

	return views.SettingsData{
		Notify:  notifyData,
		Subnets: subnets, Users: users, Values: values, Configured: configured,
		Sessions: sessions, CurrentSessionID: currentSessionID,
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
func (s *Server) backupSummary() string {
	switch {
	case s.backups != nil:
		return s.backups.Status().Summary()
	case s.store.Dialect() == store.Postgres:
		return "not available on Postgres (use pg_dump)"
	}
	return "off (set NETIS_BACKUP_DIR)"
}

func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	sn, msg := parseSubnetForm(r)
	if msg != "" {
		s.settingsError(w, r, "subnets", http.StatusBadRequest, msg)
		return
	}
	if _, err := s.store.CreateSubnet(r.Context(), sn); err != nil {
		s.settingsWriteError(w, r, "subnets", err, subnetExistsMsg)
		return
	}
	http.Redirect(w, r, "/settings?tab=subnets", http.StatusSeeOther)
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
		s.settingsError(w, r, "subnets", http.StatusBadRequest, msg)
		return
	}
	sn.ID = id
	if err := s.store.UpdateSubnet(r.Context(), sn); err != nil {
		s.settingsWriteError(w, r, "subnets", err, subnetExistsMsg)
		return
	}
	http.Redirect(w, r, "/settings?tab=subnets", http.StatusSeeOther)
}

const subnetExistsMsg = "a subnet with that CIDR already exists"

// parseSubnetForm validates and builds a store.Subnet from the request form,
// returning what is wrong with it as msg on validation failure. The
// CIDR is normalized to its masked form (e.g. "10.0.0.5/24" -> "10.0.0.0/24")
// so stored subnets are always canonical regardless of what a user typed.
func parseSubnetForm(r *http.Request) (sn store.Subnet, msg string) {
	cidr := strings.TrimSpace(r.FormValue("cidr"))
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return store.Subnet{}, "invalid CIDR"
	}
	// Every address in a subnet becomes a grid cell and a sweep target, so an
	// over-wide prefix is refused here rather than discovered when the page is
	// opened. The message names the limit and the prefix that would fit.
	if err := scan.CheckSubnetSize(cidr); err != nil {
		return store.Subnet{}, err.Error()
	}
	kind := r.FormValue("kind")
	if kind != "lan" && kind != "wireguard" && kind != "proxmox-bridge" {
		return store.Subnet{}, "bad kind"
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
	if err := s.store.DeleteSubnet(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings?tab=subnets", http.StatusSeeOther)
}

// saveIntegrationSettings writes the integration settings from a submitted
// form: a blank secret keeps the stored value, and the *_insecure checkboxes
// normalize "on" to "1". Shared by the settings page and the setup wizard.
// A value that fails validation is returned as a badInput, before anything
// from the form is written.
func (s *Server) saveIntegrationSettings(r *http.Request) error {
	// The interface name is spliced into a command the WireGuard host's shell
	// runs. Blank is fine: it falls back to wg0.
	if v := r.FormValue("wg_iface"); v != "" {
		if err := wireguard.ValidIface(v); err != nil {
			return badInput{err.Error()}
		}
	}
	for _, k := range settingsKeys {
		v := r.FormValue(k)
		if (k == "proxmox_secret" || k == "pihole_password") && v == "" {
			// Blank means "leave unchanged" — don't wipe the stored secret.
			continue
		}
		if k == "proxmox_insecure" || k == "pihole_insecure" {
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

// failSave answers an error from saveIntegrationSettings.
func (s *Server) failSave(w http.ResponseWriter, r *http.Request, err error) {
	var bad badInput
	if errors.As(err, &bad) {
		http.Error(w, bad.msg, http.StatusBadRequest)
		return
	}
	s.fail(w, r, err)
}

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
	http.Redirect(w, r, "/settings?tab=integrations", http.StatusSeeOther)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	role := r.FormValue("role")
	if username == "" || len(password) < minPasswordLen {
		s.settingsError(w, r, "users", http.StatusBadRequest, "username required, password min "+strconv.Itoa(minPasswordLen)+" chars")
		return
	}
	if len(password) > maxPasswordLen {
		s.settingsError(w, r, "users", http.StatusBadRequest, passwordTooLongMsg)
		return
	}
	if role != "admin" && role != "viewer" {
		s.settingsError(w, r, "users", http.StatusBadRequest, "bad role")
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
	http.Redirect(w, r, "/settings?tab=users", http.StatusSeeOther)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
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
		s.settingsError(w, r, "users", http.StatusBadRequest, "cannot delete the last admin")
		return
	}
	http.Redirect(w, r, "/settings?tab=users", http.StatusSeeOther)
}

func (s *Server) handleGeneralSave(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.FormValue("offline_after"))
	if err != nil || n < 1 || n > 10 {
		s.settingsError(w, r, "general", http.StatusBadRequest, "offline_after must be an integer 1-10")
		return
	}
	if err := s.store.SetSetting(r.Context(), "offline_after", strconv.Itoa(n)); err != nil {
		s.fail(w, r, err)
		return
	}
	if v := r.FormValue("default_scan_interval_sec"); v != "" {
		iv, err := strconv.Atoi(v)
		if err != nil || iv < 30 {
			s.settingsError(w, r, "general", http.StatusBadRequest, "default scan interval must be an integer >= 30")
			return
		}
		if err := s.store.SetSetting(r.Context(), "default_scan_interval_sec", strconv.Itoa(iv)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("default_subnet_kind"); v != "" {
		if v != "lan" && v != "wireguard" && v != "proxmox-bridge" {
			s.settingsError(w, r, "general", http.StatusBadRequest, "bad subnet kind")
			return
		}
		if err := s.store.SetSetting(r.Context(), "default_subnet_kind", v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("default_scan_enabled"); v != "" {
		if v != "on" && v != "off" {
			s.settingsError(w, r, "general", http.StatusBadRequest, "bad default scan enabled")
			return
		}
		if err := s.store.SetSetting(r.Context(), "default_scan_enabled", v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if v := r.FormValue("presence_fallback"); v != "" {
		if v != "on" && v != "off" {
			http.Error(w, "bad presence fallback", 400)
			return
		}
		if err := s.store.SetSetting(r.Context(), "presence_fallback", v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	// Retention windows, in days. Zero is meaningful — keep forever — so it is
	// accepted rather than treated as unset.
	for _, k := range []string{"event_retention_days", "availability_retention_days"} {
		v := r.FormValue(k)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.settingsError(w, r, "general", http.StatusBadRequest, k+" must be a non-negative integer")
			return
		}
		if err := s.store.SetSetting(r.Context(), k, strconv.Itoa(n)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/settings?tab=general", http.StatusSeeOther)
}
