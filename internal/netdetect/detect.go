package netdetect

import (
	"net"
	"net/netip"
	"sort"
	"strings"
)

type Detected struct {
	CIDR  string
	Iface string
}

type ifaceInfo struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []netip.Prefix
}

var virtualPrefixes = []string{"docker", "veth", "br-", "tap", "cni", "virbr", "lo"}

// DetectSubnets returns scannable IPv4 subnets from the host's interfaces.
func DetectSubnets() ([]Detected, error) {
	return detectFrom(systemInterfaces)
}

func detectFrom(lister func() ([]ifaceInfo, error)) ([]Detected, error) {
	ifaces, err := lister()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []Detected
	for _, ifc := range ifaces {
		if !ifc.Up || ifc.Loopback || hasVirtualPrefix(ifc.Name) {
			continue
		}
		for _, p := range ifc.Addrs {
			a := p.Addr()
			if !a.Is4() || a.IsLinkLocalUnicast() {
				continue
			}
			if p.Bits() > 30 { // skip /31 and /32 host routes
				continue
			}
			cidr := p.Masked().String()
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			out = append(out, Detected{CIDR: cidr, Iface: ifc.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CIDR < out[j].CIDR })
	return out, nil
}

func hasVirtualPrefix(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// systemInterfaces is the production lister wrapping net.Interfaces().
func systemInterfaces() ([]ifaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]ifaceInfo, 0, len(ifaces))
	for _, ifc := range ifaces {
		info := ifaceInfo{
			Name:     ifc.Name,
			Up:       ifc.Flags&net.FlagUp != 0,
			Loopback: ifc.Flags&net.FlagLoopback != 0,
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			out = append(out, info)
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			p, err := netip.ParsePrefix(ipnet.String())
			if err != nil {
				continue
			}
			info.Addrs = append(info.Addrs, p)
		}
		out = append(out, info)
	}
	return out, nil
}
