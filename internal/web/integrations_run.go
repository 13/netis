package web

import (
	"context"
	"net/http"
	"time"

	"netis/internal/store"
	"netis/internal/web/views"
)

var integrationTitles = map[string]string{
	"proxmox":   "Proxmox",
	"wireguard": "WireGuard",
	"pihole":    "Pi-hole",
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
		views.ScanToast("integration run not available").Render(r.Context(), w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	runErr := s.runner.Run(ctx, name)

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
	views.ScanToast(msg).Render(r.Context(), w)
}
