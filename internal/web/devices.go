package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"netis/internal/store"
)

var validKinds = map[string]bool{"computer": true, "switch": true, "phone": true,
	"server": true, "printer": true, "iot": true, "vm": true, "lxc": true,
	"wg-peer": true, "router": true, "modem": true, "other": true}

// normMAC parses a 48-bit MAC in any of the usual spellings (colons, dashes
// or Cisco dots) into the lowercase colon form the store matches on. ok is
// false for anything else, including the right length made of non-hex.
func normMAC(in string) (string, bool) {
	hw, err := net.ParseMAC(strings.TrimSpace(in))
	if err != nil || len(hw) != 6 {
		return "", false
	}
	return hw.String(), true
}

// parseTags splits a comma-separated tags field into trimmed, non-empty names.
func parseTags(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isHTMX reports whether r came from htmx rather than a plain browser request.
func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// subnetFor returns the id of the subnet that contains ip, so a form given
// only an address comes up with its subnet chosen. When subnets nest, the
// most specific wins. Zero when ip is not an address or no subnet holds it.
func subnetFor(subnets []store.Subnet, ip string) int64 {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return 0
	}
	var best int64
	bits := -1
	for _, sn := range subnets {
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil || !p.Contains(addr) || p.Bits() <= bits {
			continue
		}
		best, bits = sn.ID, p.Bits()
	}
	return best
}
