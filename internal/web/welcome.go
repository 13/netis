package web

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"netis/internal/netdetect"
	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
)

// Settings the first-run flow keeps. "onboarded" lets the rest of the app
// open; "setup_skipped" says the admin left the flow with Skip for now, so
// the dashboard offers to finish it; "setup_dismissed" says they closed that
// offer for good.
const (
	settingOnboarded      = "onboarded"
	settingSetupSkipped   = "setup_skipped"
	settingSetupDismissed = "setup_dismissed"
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

// welcomeNetwork gathers step 2 of the flow: the subnets netis already scans
// (when the admin comes back to it) and the ones found on this host, each
// with its size in words.
func (s *Server) welcomeNetwork(ctx context.Context) (views.WelcomeNetworkData, error) {
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return views.WelcomeNetworkData{}, err
	}
	d := views.WelcomeNetworkData{Existing: subnets}
	for _, det := range s.availableDetected(ctx) {
		d.Detected = append(d.Detected, views.DetectedSubnet{
			CIDR: det.CIDR, Iface: det.Iface, Hosts: hostCount(det.CIDR),
			Checked: scan.CheckSubnetSize(det.CIDR) == nil,
		})
	}
	return d, nil
}

// hostCount is how many host addresses cidr holds, 0 when it is not a
// prefix or too wide to count.
func hostCount(cidr string) int {
	p, err := netip.ParsePrefix(cidr)
	if err != nil || scan.CheckSubnetSize(cidr) != nil {
		return 0
	}
	n := 1 << (p.Addr().BitLen() - p.Bits())
	if p.Addr().Is4() && n > 2 {
		n -= 2 // network and broadcast
	}
	return n
}

func (s *Server) handleWelcome(w http.ResponseWriter, r *http.Request) {
	d, err := s.welcomeNetwork(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.WelcomeNetwork(d))
}

// detectedSubnet validates a subnet picked or typed in the wizard with the
// same rules the settings form applies, returning a badInput for a CIDR that
// does not parse or is too wide to enumerate.
func detectedSubnet(cidr, iface string) (store.Subnet, error) {
	cidr = strings.TrimSpace(cidr)
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return store.Subnet{}, badInput{strconv.Quote(cidr) + " is not a subnet. Write it as an address and a prefix length, like 192.168.1.0/24."}
	}
	if err := scan.CheckSubnetSize(cidr); err != nil {
		var big *scan.SubnetTooLargeError
		if errors.As(err, &big) {
			return store.Subnet{}, badInput{"That range is too large to scan. Use /" + strconv.Itoa(big.Bits) + " or narrower."}
		}
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
		http.Error(w, "the form could not be read; reload the page and try again", 400)
		return
	}
	// Validate everything first so a bad manual entry does not leave the
	// detected subnets half created.
	var subnets []store.Subnet
	for _, v := range r.Form["subnet"] {
		cidr, iface, _ := strings.Cut(v, "|")
		sn, err := detectedSubnet(cidr, iface)
		if err != nil {
			s.welcomeNetworkError(w, r, err)
			return
		}
		subnets = append(subnets, sn)
	}
	if m := strings.TrimSpace(r.FormValue("manual_cidr")); m != "" {
		sn, err := detectedSubnet(m, "")
		if err != nil {
			s.welcomeNetworkError(w, r, err)
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

// welcomeNetworkError shows step 2 again with what was wrong next to the
// field it came from, keeping what the admin picked and typed.
func (s *Server) welcomeNetworkError(w http.ResponseWriter, r *http.Request, err error) {
	var bad badInput
	if !errors.As(err, &bad) {
		s.fail(w, r, err)
		return
	}
	d, derr := s.welcomeNetwork(r.Context())
	if derr != nil {
		s.fail(w, r, derr)
		return
	}
	picked := make(map[string]bool)
	for _, v := range r.Form["subnet"] {
		cidr, _, _ := strings.Cut(v, "|")
		picked[cidr] = true
	}
	for i := range d.Detected {
		d.Detected[i].Checked = picked[d.Detected[i].CIDR]
	}
	d.Manual, d.Error = strings.TrimSpace(r.FormValue("manual_cidr")), bad.msg
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	s.render(w, r, views.WelcomeNetwork(d))
}

// integrationValues reads the integration settings as the forms show them:
// every key but the secrets, and which integrations are set up.
func (s *Server) integrationValues(ctx context.Context) (map[string]string, map[string]bool, error) {
	values := make(map[string]string)
	for _, k := range settingsKeys {
		if formSecrets[k] {
			// Never echo secrets back into the form.
			continue
		}
		v, err := s.store.GetSetting(ctx, k)
		if err != nil {
			return nil, nil, err
		}
		values[k] = v
	}
	return values, configuredIntegrations(values), nil
}

// configuredIntegrations says which integrations have their address set.
func configuredIntegrations(values map[string]string) map[string]bool {
	return map[string]bool{
		"proxmox":   values["proxmox_url"] != "",
		"wireguard": values["wg_ssh_addr"] != "",
		"pihole":    values["pihole_url"] != "",
		"adguard":   values["adguard_url"] != "",
		"opnsense":  values["opnsense_url"] != "",
	}
}

func (s *Server) handleWelcomeIntegrationsPage(w http.ResponseWriter, r *http.Request) {
	values, configured, err := s.integrationValues(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.WelcomeIntegrations(views.WelcomeServicesData{Values: values, Configured: configured}))
}

func (s *Server) handleWelcomeIntegrations(w http.ResponseWriter, r *http.Request) {
	if err := s.saveIntegrationSettings(r); err != nil {
		var bad badInput
		if !errors.As(err, &bad) {
			s.fail(w, r, err)
			return
		}
		values, configured, verr := s.integrationValues(r.Context())
		if verr != nil {
			s.fail(w, r, verr)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, r, views.WelcomeIntegrations(views.WelcomeServicesData{Values: values, Configured: configured, Error: bad.msg}))
		return
	}
	// The flow is complete: the first scan starts now rather than on the
	// scheduler's next tick, so the last step has something to show.
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.trigger != nil {
		for _, sn := range subnets {
			if sn.Kind != "wireguard" {
				s.trigger.Trigger(sn.ID)
			}
		}
	}
	for k, v := range map[string]string{settingOnboarded: "1", settingSetupSkipped: ""} {
		if err := s.store.SetSetting(r.Context(), k, v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/welcome/done", http.StatusSeeOther)
}

// handleWelcomeDone is the last step: the first scan running, a live count
// of what it finds, Run now for the services just connected, and the way on
// to the dashboard.
func (s *Server) handleWelcomeDone(w http.ResponseWriter, r *http.Request) {
	p, err := s.welcomeProgress(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, configured, err := s.integrationValues(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var services []views.Integration
	for _, it := range views.Integrations(configured, nil) {
		if it.Configured {
			services = append(services, it)
		}
	}
	s.render(w, r, views.WelcomeDone(p, services))
}

// handleWelcomeProgress is the scan count on the last step, fetched again
// whenever a scan finishes or a device turns up.
func (s *Server) handleWelcomeProgress(w http.ResponseWriter, r *http.Request) {
	p, err := s.welcomeProgress(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.WelcomeProgressBody(p))
}

func (s *Server) welcomeProgress(ctx context.Context) (views.WelcomeProgress, error) {
	var p views.WelcomeProgress
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return p, err
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return p, err
	}
	p.Devices = len(devices)
	for _, sn := range subnets {
		if sn.Kind == "wireguard" {
			continue
		}
		p.Subnets = append(p.Subnets, s.scanState(sn))
	}
	return p, nil
}

// handleWelcomeSkip leaves the flow for the dashboard. The app opens, and
// the dashboard offers to finish what is missing until it is done or
// dismissed.
func (s *Server) handleWelcomeSkip(w http.ResponseWriter, r *http.Request) {
	for k, v := range map[string]string{settingOnboarded: "1", settingSetupSkipped: "1", settingSetupDismissed: ""} {
		if err := s.store.SetSetting(r.Context(), k, v); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleSetupDismiss closes the dashboard's Finish setup panel for good.
func (s *Server) handleSetupDismiss(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSetting(r.Context(), settingSetupDismissed, "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setupStatus is the dashboard's Finish setup panel for an admin, nil when
// there is nothing to offer: it shows after Skip for now, or whenever no
// subnet exists, until everything is in place or the admin dismisses it.
func (s *Server) setupStatus(r *http.Request, subnets []store.Subnet) (*views.SetupStatus, error) {
	if !isAdmin(r) {
		return nil, nil
	}
	ctx := r.Context()
	if v, err := s.store.GetSetting(ctx, settingSetupDismissed); err != nil || v == "1" {
		return nil, err
	}
	skipped, err := s.store.GetSetting(ctx, settingSetupSkipped)
	if err != nil {
		return nil, err
	}
	if skipped != "1" && len(subnets) > 0 {
		return nil, nil
	}
	st := views.SetupStatus{NoSubnets: len(subnets) == 0}
	if !st.NoSubnets {
		statuses, err := s.store.ListIntegrationStatus(ctx)
		if err != nil {
			return nil, err
		}
		// A WireGuard subnet is read from its server, never swept, so only
		// the others can be waiting for a first scan.
		for _, sn := range subnets {
			if sn.Kind == "wireguard" {
				continue
			}
			st.NoScan = true
			if x := s.scanState(sn); x.Queued || x.Running {
				st.Scanning = true
			}
		}
		for _, it := range statuses {
			if it.Name == "scan" {
				st.NoScan = false
			}
		}
	}
	_, configured, err := s.integrationValues(ctx)
	if err != nil {
		return nil, err
	}
	st.NoIntegrations = true
	for _, on := range configured {
		if on {
			st.NoIntegrations = false
		}
	}
	if !st.NoSubnets && !st.NoScan && !st.NoIntegrations {
		return nil, nil
	}
	return &st, nil
}
