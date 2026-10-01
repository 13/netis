package web

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
	"netis/internal/wol"
)

func (s *Server) handleLinkAdd(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	label := strings.TrimSpace(r.FormValue("label"))
	url := strings.TrimSpace(r.FormValue("url"))
	if label != "" && url != "" {
		if _, err := s.store.AddLink(r.Context(), id, label, url); err != nil {
			s.fail(w, r, err)
			return
		}
		s.flashToast(w, r, "Link added")
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleLinkDelete(w http.ResponseWriter, r *http.Request) {
	linkID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	// The device id isn't in this path (/links/{id}/delete), so redirect
	// target comes from the form's referring device id.
	devID := r.FormValue("device_id")
	if err := s.store.DeleteLink(r.Context(), linkID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Link deleted")
	http.Redirect(w, r, "/devices/"+devID, http.StatusSeeOther)
}

func (s *Server) handleFieldSet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	key := strings.TrimSpace(r.FormValue("key"))
	if key != "" {
		if err := s.store.SetCustomField(r.Context(), id, key, r.FormValue("value")); err != nil {
			s.fail(w, r, err)
			return
		}
		s.flashToast(w, r, "Field saved")
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleFieldDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	key := r.FormValue("key")
	if err := s.store.DeleteCustomField(r.Context(), id, key); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Field deleted")
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

// handleWOL sends a Wake-on-LAN magic packet to the MAC of the device's
// first interface that has one. It goes to the directed broadcast of every
// subnet that interface has an address in, so a host on another VLAN is
// reached, and to 255.255.255.255 as well. An htmx request is answered with a
// toast naming the addresses used; a plain form post redirects back to the
// device page.
func (s *Server) handleWOL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	ifaces, err := s.store.ListIfaces(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, f := range ifaces {
		if f.MAC == nil || *f.MAC == "" {
			continue
		}
		targets, err := s.wolTargets(r.Context(), f.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		sent, err := wol.SendAll(*f.MAC, targets, s.wolSend)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if isHTMX(r) {
			s.render(w, r, views.ScanToast("Wake on LAN sent to "+*f.MAC+" via "+wol.HostsOf(sent)))
			return
		}
		s.flashToast(w, r, "Wake on LAN sent to "+*f.MAC)
		http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
		return
	}
	http.Error(w, "this device has no MAC address, so it cannot be woken", http.StatusBadRequest)
}

// wolTargets returns where a magic packet for an interface goes: the directed
// broadcast of the subnet of each of its IPs (the subnet's prefix defines the
// broadcast, not the address), then the limited broadcast.
func (s *Server) wolTargets(ctx context.Context, ifaceID int64) ([]string, error) {
	ips, err := s.store.ListIPs(ctx, ifaceID)
	if err != nil {
		return nil, err
	}
	var prefixes []netip.Prefix
	for _, ip := range ips {
		sn, err := s.store.GetSubnet(ctx, ip.SubnetID)
		if err != nil {
			return nil, err
		}
		if p, err := netip.ParsePrefix(sn.CIDR); err == nil {
			prefixes = append(prefixes, p)
		}
	}
	return wol.Targets(prefixes), nil
}

// handlePortScan runs an on-demand TCP port scan against the first IP of
// the device's first interface, records the open ports it found in place of
// the previous result, and redirects back to the device page.
func (s *Server) handlePortScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	ifaces, err := s.store.ListIfaces(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(ifaces) == 0 {
		http.Error(w, "this device has no network interface, so there is nothing to scan; add a MAC or IP address first", 400)
		return
	}
	ips, err := s.store.ListIPs(r.Context(), ifaces[0].ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(ips) == 0 {
		http.Error(w, "this device has no IP address, so its ports cannot be scanned; add an IP address first", 400)
		return
	}
	open := scan.PortScan(r.Context(), ips[0].IP, scan.CommonPorts, time.Second)
	found := make([]store.OpenPort, 0, len(open))
	for _, p := range open {
		found = append(found, store.OpenPort{Port: p, ServiceGuess: scan.ServiceGuess(p)})
	}
	// The scan is the whole truth for the ports it probes, so it replaces the
	// recorded set: a port that has closed since the last scan disappears.
	if err := s.store.ReplaceOpenPorts(r.Context(), ifaces[0].ID, "tcp", found,
		time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.fail(w, r, err)
		return
	}
	// Open ports are evidence for autofill; run it for this device now so
	// the page the user lands on already shows what they imply.
	if s.autofill != nil {
		if err := s.autofill.Run(r.Context(), id); err != nil {
			slog.Error("autofill after port scan", "device_id", id, "err", err)
		}
	}
	s.flashToast(w, r, portScanToast(ips[0].IP, len(found)))
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

// portScanToast says what a port scan found, repeating what was done.
func portScanToast(ip string, open int) string {
	if open == 0 {
		return "Ports scanned on " + ip + ": none open"
	}
	return "Ports scanned on " + ip + ": " + strconv.Itoa(open) + " open"
}

// handleDeviceIPKind flips an IP assignment's lease kind (static/dhcp) from the
// device detail page and returns the re-rendered toggle control.
func (s *Server) handleDeviceIPKind(w http.ResponseWriter, r *http.Request) {
	devID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	subnetID, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	if err != nil {
		http.Error(w, "that subnet does not exist; reload the page and try again", 400)
		return
	}
	ip := r.FormValue("ip")
	kind := r.FormValue("kind")
	if kind != "static" && kind != "dhcp" {
		http.Error(w, "choose static or DHCP", 400)
		return
	}
	if err := s.store.SetIPKind(r.Context(), subnetID, ip, kind); err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.LeaseToggle(devID, subnetID, ip, kind))
}
