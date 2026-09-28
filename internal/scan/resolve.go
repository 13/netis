package scan

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

// rdnsTimeout bounds the reverse DNS lookup; mdnsTimeout bounds the mDNS
// fallback that follows it when DNS has no name.
const (
	rdnsTimeout = 500 * time.Millisecond
	mdnsTimeout = 400 * time.Millisecond
)

// mdnsAddr is the IPv4 mDNS group and port.
const mdnsAddr = "224.0.0.251:5353"

// ResolveName finds a name for ip: reverse DNS first, then an mDNS reverse
// lookup, which is where phones, printers, Macs and most Linux boxes running
// Avahi announce themselves when the router's DNS knows nothing about them.
func ResolveName(ctx context.Context, ip string) string {
	if n := reverseDNS(ctx, ip); n != "" {
		return n
	}
	return mdnsLookupAddr(ctx, ip, mdnsAddr, mdnsTimeout)
}

func reverseDNS(ctx context.Context, ip string) string {
	ctx, cancel := context.WithTimeout(ctx, rdnsTimeout)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// reverseName returns the in-addr.arpa name for an IPv4 address, or "" for
// anything else: mDNS reverse lookups are only attempted for IPv4.
func reverseName(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is4() {
		return ""
	}
	b := a.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", b[3], b[2], b[1], b[0])
}

// qu is the unicast-response bit of an mDNS question's class (RFC 6762 5.4):
// it asks the responder to answer straight back to the querier.
const qu = 0x8000

// buildMDNSQuery builds a one-question PTR query for name with the QU bit set.
func buildMDNSQuery(name string) ([]byte, error) {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{
		Name: n, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET | qu,
	}); err != nil {
		return nil, err
	}
	return b.Finish()
}

// parseMDNSAnswer returns the PTR target answering name in msg, without its
// trailing dot, or "" when msg is not a response carrying one.
func parseMDNSAnswer(msg []byte, name string) string {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response {
		return ""
	}
	if err := p.SkipAllQuestions(); err != nil {
		return ""
	}
	for {
		ah, err := p.AnswerHeader()
		if err != nil {
			return ""
		}
		if ah.Type != dnsmessage.TypePTR || !strings.EqualFold(ah.Name.String(), name) {
			if err := p.SkipAnswer(); err != nil {
				return ""
			}
			continue
		}
		r, err := p.PTRResource()
		if err != nil {
			return ""
		}
		return strings.TrimSuffix(r.PTR.String(), ".")
	}
}

// mdnsLookupAddr sends a PTR query for ip's reverse name to addr (the mDNS
// group in production, a local responder in tests) and waits up to timeout
// for a unicast answer on the same socket.
func mdnsLookupAddr(ctx context.Context, ip, addr string, timeout time.Duration) string {
	name := reverseName(ip)
	if name == "" {
		return ""
	}
	query, err := buildMDNSQuery(name)
	if err != nil {
		return ""
	}
	dst, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return ""
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return ""
	}
	defer conn.Close()
	if dst.IP.IsMulticast() {
		// The group is link-local: send it out of the interface the target
		// lives on rather than wherever the default route points.
		pc := ipv4.NewPacketConn(conn)
		if ifi := ifaceFor(ip); ifi != nil {
			_ = pc.SetMulticastInterface(ifi)
		}
		_ = pc.SetMulticastTTL(255)
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return ""
	}
	if _, err := conn.WriteToUDP(query, dst); err != nil {
		return ""
	}
	buf := make([]byte, 9000)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return ""
		}
		if got := parseMDNSAnswer(buf[:n], name); got != "" {
			return got
		}
	}
}

// ifaceFor returns the up, multicast-capable interface with an address on
// ip's subnet, or nil.
func ifaceFor(ip string) *net.Interface {
	target := net.ParseIP(ip)
	ifs, err := net.Interfaces()
	if err != nil || target == nil {
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
			if n, ok := a.(*net.IPNet); ok && n.Contains(target) {
				return ifi
			}
		}
	}
	return nil
}
