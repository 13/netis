package web

import (
	"context"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"netis/internal/netdetect"
	"netis/internal/scan"
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
	s.render(w, r, views.WelcomeSubnets(u.Username, s.availableDetected(r.Context())))
}

// detectedSubnet validates a subnet picked or typed in the wizard with the
// same rules the settings form applies, returning a badInput for a CIDR that
// does not parse or is too wide to enumerate.
func detectedSubnet(cidr, iface string) (store.Subnet, error) {
	cidr = strings.TrimSpace(cidr)
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return store.Subnet{}, badInput{"invalid CIDR " + strconv.Quote(cidr)}
	}
	if err := scan.CheckSubnetSize(cidr); err != nil {
		return store.Subnet{}, badInput{err.Error()}
	}
	name := iface
	if name == "" {
		name = prefix.Masked().String()
	}
	return store.Subnet{
		CIDR: prefix.Masked().String(), Name: name, Kind: "lan",
		ScanEnabled: true, ScanIntervalSec: 120,
	}, nil
}

func (s *Server) handleWelcomeSubnets(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form data", 400)
		return
	}
	// Validate everything first so a bad manual entry does not leave the
	// detected subnets half created.
	var subnets []store.Subnet
	for _, v := range r.Form["subnet"] {
		cidr, iface, _ := strings.Cut(v, "|")
		sn, err := detectedSubnet(cidr, iface)
		if err != nil {
			s.failSave(w, r, err)
			return
		}
		subnets = append(subnets, sn)
	}
	if m := strings.TrimSpace(r.FormValue("manual_cidr")); m != "" {
		sn, err := detectedSubnet(m, "")
		if err != nil {
			s.failSave(w, r, err)
			return
		}
		subnets = append(subnets, sn)
	}
	for _, sn := range subnets {
		// A subnet that already exists (a resubmitted wizard page, or a manual
		// entry repeating a detected one) is what the user wanted anyway.
		if _, err := s.store.CreateSubnet(r.Context(), sn); err != nil && !store.IsUniqueViolation(err) {
			s.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/welcome/integrations", http.StatusSeeOther)
}

func (s *Server) handleWelcomeIntegrationsPage(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	s.render(w, r, views.WelcomeIntegrations(u.Username, map[string]string{}))
}

func (s *Server) handleWelcomeIntegrations(w http.ResponseWriter, r *http.Request) {
	if err := s.saveIntegrationSettings(r); err != nil {
		s.failSave(w, r, err)
		return
	}
	s.finishOnboarding(w, r)
}

func (s *Server) handleWelcomeSkip(w http.ResponseWriter, r *http.Request) {
	s.finishOnboarding(w, r)
}

func (s *Server) finishOnboarding(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSetting(r.Context(), "onboarded", "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
