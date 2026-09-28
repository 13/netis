package wol

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// DirectedBroadcast returns the broadcast address of an IPv4 prefix: its
// network address with every host bit set. A packet sent there is routed
// toward that subnet and delivered to every host on it, which is how a host on
// another VLAN is reached; the limited broadcast 255.255.255.255 only leaves
// through the interface the default route uses. IPv6 has no broadcast, and a
// /31 or /32 has no broadcast address, so those report false.
func DirectedBroadcast(p netip.Prefix) (netip.Addr, bool) {
	if !p.IsValid() || !p.Addr().Is4() || p.Bits() > 30 {
		return netip.Addr{}, false
	}
	a := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
	v |= host
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
}

// Targets returns the UDP addresses a magic packet is sent to: the directed
// broadcast of each prefix that has one, in the order given and without
// repeats, followed by the limited broadcast BroadcastAddr.
func Targets(prefixes []netip.Prefix) []string {
	var out []string
	seen := make(map[netip.Addr]bool)
	for _, p := range prefixes {
		b, ok := DirectedBroadcast(p)
		if !ok || seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, net.JoinHostPort(b.String(), "9"))
	}
	return append(out, BroadcastAddr)
}

// SendAll sends the magic packet for mac to each address with send (SendTo in
// production) and returns the addresses it went to. It fails only when it
// went nowhere, with every address's error joined. A malformed MAC fails
// before anything is sent.
func SendAll(mac string, addrs []string, send func(mac, addr string) error) ([]string, error) {
	if _, err := BuildMagicPacket(mac); err != nil {
		return nil, err
	}
	var sent []string
	var errs []error
	for _, a := range addrs {
		if err := send(mac, a); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a, err))
			continue
		}
		sent = append(sent, a)
	}
	if len(sent) == 0 {
		return nil, fmt.Errorf("magic packet not sent: %w", errors.Join(errs...))
	}
	return sent, nil
}

// HostsOf strips the ":9" port from each address, for showing where a packet
// went.
func HostsOf(addrs []string) string {
	hosts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if h, _, err := net.SplitHostPort(a); err == nil {
			a = h
		}
		hosts = append(hosts, a)
	}
	return strings.Join(hosts, ", ")
}
