package web

import (
	"errors"
	"net/http"

	"netis/internal/store"
	"netis/internal/web/views"
)

var integrationTitles = map[string]string{
	"proxmox":   "Proxmox",
	"wireguard": "WireGuard",
	"pihole":    "Pi-hole",
	"adguard":   "AdGuard Home",
	"opnsense":  "OPNsense",
}

// handleIntegrationRun triggers a single on-demand run of an integration and
// toasts the result read back from the recorded status.
func (s *Server) handleIntegrationRun(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	title, ok := integrationTitles[name]
	if !ok {
		http.Error(w, "unknown integration", 400)
		return
	}
	if s.runner == nil {
		s.render(w, r, views.ScanToast("integration run not available"))
		return
	}
	// The runner applies its own per-run deadline.
	runErr := s.runner.Run(r.Context(), name)
	if errors.Is(runErr, ErrIntegrationBusy) {
		s.render(w, r, views.ScanToast(title+": already running"))
		return
	}

	var st *store.IntegrationStatus
	if list, err := s.store.ListIntegrationStatus(r.Context()); err == nil {
		for i := range list {
			if list[i].Name == name {
				st = &list[i]
				break
			}
		}
	}
	var msg string
	switch {
	case st == nil && runErr != nil:
		msg = title + ": not configured"
	case st == nil:
		msg = title + ": ran"
	case st.OK:
		msg = title + ": connected"
		if st.Detail != "" {
			msg += " · " + st.Detail
		}
	default:
		msg = title + ": failing"
		if st.Detail != "" {
			msg += " · " + st.Detail
		}
	}
	s.render(w, r, views.ScanToast(msg))
}
