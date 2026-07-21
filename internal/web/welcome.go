package web

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"netis/internal/netdetect"
	"netis/internal/store"
	"netis/internal/web/views"
)

func (s *Server) availableDetected(ctx context.Context) []netdetect.Detected {
	detected, err := s.detect()
	if err != nil {
		return nil
	}
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return nil
	}
	have := make(map[string]bool, len(subnets))
	for _, sn := range subnets {
		have[sn.CIDR] = true
	}
	var out []netdetect.Detected
	for _, d := range detected {
		if !have[d.CIDR] {
			out = append(out, d)
		}
	}
	return out
}

func (s *Server) handleWelcome(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	views.WelcomeSubnets(u.Username, s.availableDetected(r.Context())).Render(r.Context(), w)
}

func (s *Server) createDetectedSubnet(ctx context.Context, cidr, iface string) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return
	}
	name := iface
	if name == "" {
		name = prefix.Masked().String()
	}
	s.store.CreateSubnet(ctx, store.Subnet{
		CIDR: prefix.Masked().String(), Name: name, Kind: "lan",
		ScanEnabled: true, ScanIntervalSec: 120,
	})
}

func (s *Server) handleWelcomeSubnets(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	for _, v := range r.Form["subnet"] {
		cidr, iface, _ := strings.Cut(v, "|")
		s.createDetectedSubnet(r.Context(), cidr, iface)
	}
	if m := strings.TrimSpace(r.FormValue("manual_cidr")); m != "" {
		s.createDetectedSubnet(r.Context(), m, "")
	}
	http.Redirect(w, r, "/welcome/integrations", http.StatusSeeOther)
}

func (s *Server) handleWelcomeIntegrationsPage(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	views.WelcomeIntegrations(u.Username, map[string]string{}).Render(r.Context(), w)
}

func (s *Server) handleWelcomeIntegrations(w http.ResponseWriter, r *http.Request) {
	if err := s.saveIntegrationSettings(r); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.finishOnboarding(w, r)
}

func (s *Server) handleWelcomeSkip(w http.ResponseWriter, r *http.Request) {
	s.finishOnboarding(w, r)
}

func (s *Server) finishOnboarding(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSetting(r.Context(), "onboarded", "1"); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
