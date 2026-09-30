// Package probe asks devices on a local link to describe themselves over
// discovery protocols (mDNS service browsing, SSDP) and reports what they say.
package probe

import (
	"net"
	"net/netip"
)

// InterfaceFor returns the up, multicast-capable interface that has an
// address inside cidr, or nil (a routed subnet, or none matches).
func InterfaceFor(cidr string) *net.Interface {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil
	}
	prefix = prefix.Masked()
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifs {
		ifi := &ifs[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP)
			if ok && prefix.Contains(ip.Unmap()) {
				return ifi
			}
		}
	}
	return nil
}
