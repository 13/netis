package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"netis/internal/scan"
	"netis/internal/store"
)

func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	sn, msg := parseSubnetForm(r)
	if msg != "" {
		s.settingsError(w, r, "network", http.StatusBadRequest, msg)
		return
	}
	if sn.DHCPStart != "" {
		sn.DHCPPoolSource = "user"
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
	old, err := s.store.GetSubnet(r.Context(), id)
	if err != nil {
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
	sn.DHCPPoolSource = poolSource(old, sn)
	auditNote(r).Target = "subnet " + sn.CIDR
	if err := s.store.UpdateSubnet(r.Context(), sn); err != nil {
		s.settingsWriteError(w, r, "network", err, subnetExistsMsg)
		return
	}
	http.Redirect(w, r, "/settings/network", http.StatusSeeOther)
}

// poolSource is who owns the pool the subnet form saves: whoever owned it
// while it is unchanged, the user once it is edited, and nobody when it is
// cleared, so a DHCP integration may fill it again.
func poolSource(old, sn store.Subnet) string {
	switch {
	case sn.DHCPStart == "" && sn.DHCPEnd == "":
		return ""
	case sn.DHCPStart == old.DHCPStart && sn.DHCPEnd == old.DHCPEnd:
		return old.DHCPPoolSource
	default:
		return "user"
	}
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
	start, end, msg := parseDHCPPool(prefix.Masked(), r.FormValue("dhcp_start"), r.FormValue("dhcp_end"))
	if msg != "" {
		return store.Subnet{}, msg
	}
	return store.Subnet{
		CIDR: prefix.Masked().String(), Name: r.FormValue("name"), Kind: kind,
		ScanEnabled: r.FormValue("scan_enabled") == "on", ScanIntervalSec: interval,
		DHCPStart: start, DHCPEnd: end,
	}, ""
}

// parseDHCPPool checks a subnet's DHCP pool: both ends blank (no pool), or
// two addresses of the subnet with the first no higher than the last. A
// last octet alone ("100") is read as that address in the subnet, the way
// the pool is usually written on a router.
func parseDHCPPool(prefix netip.Prefix, start, end string) (string, string, string) {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	if start == "" && end == "" {
		return "", "", ""
	}
	if start == "" || end == "" {
		return "", "", "give the DHCP pool both a first and a last address, or leave both empty"
	}
	lo, ok1 := poolAddr(prefix, start)
	hi, ok2 := poolAddr(prefix, end)
	switch {
	case !ok1 || !ok2:
		return "", "", "the DHCP pool's addresses must be in " + prefix.String() + ", like " + prefix.Addr().Next().String()
	case hi.Less(lo):
		return "", "", "the DHCP pool's first address must not come after its last"
	}
	return lo.String(), hi.String(), ""
}

// poolAddr reads one end of a DHCP pool: a full address in prefix, or for an
// IPv4 subnet a last octet that completes the network address.
func poolAddr(prefix netip.Prefix, s string) (netip.Addr, bool) {
	if n, err := strconv.Atoi(s); err == nil && prefix.Addr().Is4() && n >= 0 && n <= 255 {
		b := prefix.Addr().As4()
		b[3] = byte(n)
		a := netip.AddrFrom4(b)
		return a, prefix.Contains(a)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a, prefix.Contains(a)
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
