package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/store"
	"netis/internal/web/views"
)

// settingsKeys is the fixed set of settings written by the integrations
// form. proxmox_secret is handled specially: it's never echoed back into the
// form, and posting a blank value keeps the existing stored secret.
var settingsKeys = []string{
	"proxmox_url", "proxmox_token_id", "proxmox_secret", "proxmox_insecure",
	"wg_ssh_addr", "wg_ssh_user", "wg_ssh_key_path", "wg_iface",
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	users, err := s.store.ListUsers()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	values := make(map[string]string)
	for _, k := range settingsKeys {
		if k == "proxmox_secret" {
			// Never echo the secret back into the form.
			continue
		}
		v, err := s.store.GetSetting(k)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		values[k] = v
	}
	offlineAfter, err := s.store.GetSetting("offline_after")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	values["offline_after"] = offlineAfter

	u, _ := userFrom(r)
	views.SettingsPage(u.Username, views.SettingsData{
		Subnets: subnets, Users: users, Values: values,
	}).Render(r.Context(), w)
}

func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	sn, ok := parseSubnetForm(w, r)
	if !ok {
		return
	}
	if _, err := s.store.CreateSubnet(sn); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleSubnetUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetSubnet(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	sn, ok := parseSubnetForm(w, r)
	if !ok {
		return
	}
	sn.ID = id
	if err := s.store.UpdateSubnet(sn); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// parseSubnetForm validates and builds a store.Subnet from the request form,
// writing a 400 response and returning ok=false on validation failure. The
// CIDR is normalized to its masked form (e.g. "10.0.0.5/24" -> "10.0.0.0/24")
// so stored subnets are always canonical regardless of what a user typed.
func parseSubnetForm(w http.ResponseWriter, r *http.Request) (store.Subnet, bool) {
	cidr := strings.TrimSpace(r.FormValue("cidr"))
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		http.Error(w, "invalid CIDR", 400)
		return store.Subnet{}, false
	}
	kind := r.FormValue("kind")
	if kind != "lan" && kind != "wireguard" && kind != "proxmox-bridge" {
		http.Error(w, "bad kind", 400)
		return store.Subnet{}, false
	}
	interval, err := strconv.Atoi(r.FormValue("scan_interval_sec"))
	if err != nil || interval < 30 {
		interval = 120
	}
	return store.Subnet{
		CIDR: prefix.Masked().String(), Name: r.FormValue("name"), Kind: kind,
		ScanEnabled: r.FormValue("scan_enabled") == "on", ScanIntervalSec: interval,
	}, true
}

func (s *Server) handleSubnetDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteSubnet(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleIntegrationsSave(w http.ResponseWriter, r *http.Request) {
	for _, k := range settingsKeys {
		v := r.FormValue(k)
		if k == "proxmox_secret" && v == "" {
			// Blank means "leave unchanged" — don't wipe the stored secret.
			continue
		}
		if err := s.store.SetSetting(k, v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	role := r.FormValue("role")
	if username == "" || len(password) < 6 {
		http.Error(w, "username required, password min 6 chars", 400)
		return
	}
	if role != "admin" && role != "viewer" {
		http.Error(w, "bad role", 400)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if _, err := s.store.CreateUser(username, string(hash), role); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
	deleted, err := s.store.DeleteUserGuarded(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !deleted {
		// Distinguish "no such user" (404) from "refused: last admin"
		// (400) for a useful error response.
		users, err := s.store.ListUsers()
		if err != nil {
			http.Error(w, err.Error(), 500)
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
		http.Error(w, "cannot delete the last admin", 400)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleGeneralSave(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.FormValue("offline_after"))
	if err != nil || n < 1 || n > 10 {
		http.Error(w, "offline_after must be an integer 1-10", 400)
		return
	}
	if err := s.store.SetSetting("offline_after", strconv.Itoa(n)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
