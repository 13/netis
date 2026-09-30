package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

// mdnsAddr is the IPv4 mDNS group and port.
const mdnsAddr = "224.0.0.251:5353"

// qu is the unicast-response bit of an mDNS question's class (RFC 6762 5.4):
// it asks the responder to answer straight back to the querier.
const qu = 0x8000

// maxQuery bounds one query packet; more questions go in another packet.
const maxQuery = 1400

// MDNSService is one service instance a host announced.
type MDNSService struct {
	IP       string            // UDP source address of the answer
	Type     string            // service type without domain, e.g. "_googlecast._tcp"
	Instance string            // instance label, e.g. "Living Room TV"
	TXT      map[string]string // TXT key=value pairs, keys lower-cased; a bare key maps to ""
}

// MDNSTypes are the service types BrowseMDNS asks for by default.
var MDNSTypes = []string{
	"_googlecast._tcp", "_airplay._tcp", "_raop._tcp", "_device-info._tcp",
	"_ipp._tcp", "_ipps._tcp", "_printer._tcp", "_pdl-datastream._tcp",
	"_hap._tcp", "_hue._tcp", "_esphomelib._tcp", "_home-assistant._tcp",
	"_smb._tcp", "_sonos._tcp", "_spotify-connect._tcp", "_workstation._tcp",
	"_companion-link._tcp",
}

// BrowseMDNS asks the link behind ifi for types and collects answers for
// window. ifi nil sends without choosing an interface.
func BrowseMDNS(ctx context.Context, ifi *net.Interface, types []string, window time.Duration) ([]MDNSService, error) {
	return browseMDNS(ctx, ifi, mdnsAddr, types, window)
}

// buildQuestions builds PTR queries (QU bit set) for "<type>.local." for
// each type, splitting them so no packet exceeds maxQuery bytes.
func buildQuestions(types []string) ([][]byte, error) {
	var (
		out   [][]byte
		batch []dnsmessage.Name
		size  = 12 // header
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
		if err := b.StartQuestions(); err != nil {
			return err
		}
		for _, n := range batch {
			if err := b.Question(dnsmessage.Question{
				Name: n, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET | qu,
			}); err != nil {
				return err
			}
		}
		msg, err := b.Finish()
		if err != nil {
			return err
		}
		out = append(out, msg)
		batch, size = nil, 12
		return nil
	}
	for _, t := range types {
		n, err := dnsmessage.NewName(t + ".local.")
		if err != nil {
			return nil, err
		}
		// Wire size of a name is its dotted length plus one; four more bytes
		// carry type and class. No compression is assumed.
		q := int(n.Length) + 1 + 4
		if size+q > maxQuery {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		batch = append(batch, n)
		size += q
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildTXTQuery builds a one-question TXT query for name with the QU bit set.
func buildTXTQuery(name string) ([]byte, error) {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{
		Name: n, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET | qu,
	}); err != nil {
		return nil, err
	}
	return b.Finish()
}

// parseTXT turns TXT strings into key/value pairs: split on the first "=",
// key lower-cased; a string without "=" is a bare key mapping to "".
func parseTXT(txt []string) map[string]string {
	m := map[string]string{}
	for _, s := range txt {
		k, v, _ := strings.Cut(s, "=")
		if k == "" {
			continue
		}
		m[strings.ToLower(k)] = v
	}
	return m
}

// instKey identifies a service instance by the address that announced it
// and its lower-cased full name.
type instKey struct{ ip, name string }

// instance is a PTR answer seen for a requested type.
type instance struct {
	typ      string
	fullName string
}

// browse collects the answers of one browse.
type browse struct {
	types     map[string]string // lower-cased "<type>.local." -> type
	instances map[instKey]instance
	txt       map[instKey][]string
	asked     map[string]bool // lower-cased full names already sent a TXT question
}

// handle records the PTR and TXT records in msg from ip and returns the full
// names of new instances still lacking a TXT record.
func (br *browse) handle(ip string, msg []byte) []string {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response {
		return nil
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil
	}
	answers, err := p.AllAnswers()
	if err != nil {
		return nil
	}
	if err := p.SkipAllAuthorities(); err != nil {
		return nil
	}
	additionals, err := p.AllAdditionals()
	if err != nil {
		return nil
	}
	var fresh []instKey
	for _, r := range append(answers, additionals...) {
		name := r.Header.Name.String()
		switch body := r.Body.(type) {
		case *dnsmessage.PTRResource:
			typ, ok := br.types[strings.ToLower(name)]
			if !ok {
				continue
			}
			full := body.PTR.String()
			suffix := "." + typ + ".local."
			if len(full) <= len(suffix) || !strings.EqualFold(full[len(full)-len(suffix):], suffix) {
				continue
			}
			k := instKey{ip, strings.ToLower(full)}
			if _, seen := br.instances[k]; !seen {
				br.instances[k] = instance{typ: typ, fullName: full}
				fresh = append(fresh, k)
			}
		case *dnsmessage.TXTResource:
			br.txt[instKey{ip, strings.ToLower(name)}] = body.TXT
		}
	}
	var ask []string
	for _, k := range fresh {
		if _, ok := br.txt[k]; ok || br.asked[k.name] {
			continue
		}
		br.asked[k.name] = true
		ask = append(ask, br.instances[k].fullName)
	}
	return ask
}

// services returns one MDNSService per instance, sorted by IP, type, instance.
func (br *browse) services() []MDNSService {
	out := make([]MDNSService, 0, len(br.instances))
	for k, in := range br.instances {
		label := in.fullName[:len(in.fullName)-len("."+in.typ+".local.")]
		out = append(out, MDNSService{IP: k.ip, Type: in.typ, Instance: label, TXT: parseTXT(br.txt[k])})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IP != b.IP {
			ai, aerr := netip.ParseAddr(a.IP)
			bi, berr := netip.ParseAddr(b.IP)
			if aerr == nil && berr == nil {
				return ai.Less(bi)
			}
			return a.IP < b.IP
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Instance < b.Instance
	})
	return out
}

// browseMDNS sends PTR questions for types to addr (the mDNS group in
// production, a local responder in tests), follows up with TXT questions for
// instances announced without one, and collects answers until window passes
// or ctx ends.
func browseMDNS(ctx context.Context, ifi *net.Interface, addr string, types []string, window time.Duration) ([]MDNSService, error) {
	queries, err := buildQuestions(types)
	if err != nil {
		return nil, err
	}
	dst, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if dst.IP.IsMulticast() {
		pc := ipv4.NewPacketConn(conn)
		if ifi != nil {
			if err := pc.SetMulticastInterface(ifi); err != nil {
				return nil, err
			}
		}
		_ = pc.SetMulticastTTL(255)
	}
	deadline := time.Now().Add(window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	// A cancelled context ends the read loop like the deadline does.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()
	for _, q := range queries {
		if _, err := conn.WriteToUDP(q, dst); err != nil {
			return nil, err
		}
	}

	br := &browse{
		types:     map[string]string{},
		instances: map[instKey]instance{},
		txt:       map[instKey][]string{},
		asked:     map[string]bool{},
	}
	for _, t := range types {
		br.types[strings.ToLower(t+".local.")] = t
	}
	buf := make([]byte, 9000)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return nil, err
		}
		for _, name := range br.handle(from.IP.String(), buf[:n]) {
			// Best effort: a failed follow-up only leaves that TXT empty.
			if q, err := buildTXTQuery(name); err == nil {
				_, _ = conn.WriteToUDP(q, dst)
			}
		}
	}
	return br.services(), nil
}
