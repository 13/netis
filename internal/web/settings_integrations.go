package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

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
