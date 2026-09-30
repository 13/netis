package probe

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// responder answers mDNS queries on a loopback socket. For each PTR
// question it knows it answers with the PTR; with inlineTXT it adds the TXT
// as an additional record, otherwise only a later TXT question gets it.
// ptr and txt are fixed before serving starts, so serve reads them race-free.
type responder struct {
	conn      *net.UDPConn
	ptr       map[string]string   // "<type>.local." -> instance full name
	txt       map[string][]string // instance full name -> TXT strings
	inlineTXT bool
	txtAsked  chan string
}

func newResponder(t *testing.T, inline bool, ptr map[string]string, txt map[string][]string) *responder {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r := &responder{conn: c, ptr: ptr, txt: txt, inlineTXT: inline, txtAsked: make(chan string, 8)}
	t.Cleanup(func() { c.Close() })
	go r.serve()
	return r
}

func (r *responder) addr() string { return r.conn.LocalAddr().String() }

func (r *responder) serve() {
	buf := make([]byte, 9000)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var p dnsmessage.Parser
		if _, err := p.Start(buf[:n]); err != nil {
			continue
		}
		qs, _ := p.AllQuestions()
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
		b.StartAnswers()
		var extra []string
		for _, q := range qs {
			name := q.Name.String()
			switch q.Type {
			case dnsmessage.TypePTR:
				if inst, ok := r.ptr[name]; ok {
					b.PTRResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 120},
						dnsmessage.PTRResource{PTR: dnsmessage.MustNewName(inst)})
					if r.inlineTXT {
						extra = append(extra, inst)
					}
				}
			case dnsmessage.TypeTXT:
				r.txtAsked <- name
				if t, ok := r.txt[name]; ok {
					b.TXTResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 120},
						dnsmessage.TXTResource{TXT: t})
				}
			}
		}
		b.StartAdditionals()
		for _, inst := range extra {
			b.TXTResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(inst), Class: dnsmessage.ClassINET, TTL: 120},
				dnsmessage.TXTResource{TXT: r.txt[inst]})
		}
		msg, err := b.Finish()
		if err == nil {
			r.conn.WriteToUDP(msg, from)
		}
	}
}

func TestBrowseMDNSInlineTXT(t *testing.T) {
	r := newResponder(t, true,
		map[string]string{"_googlecast._tcp.local.": "Living Room TV._googlecast._tcp.local."},
		map[string][]string{"Living Room TV._googlecast._tcp.local.": {"md=Chromecast", "fn=Living Room TV", "RS"}})
	got, err := browseMDNS(context.Background(), nil, r.addr(), []string{"_googlecast._tcp", "_ipp._tcp"}, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("services = %+v", got)
	}
	s := got[0]
	if s.IP != "127.0.0.1" || s.Type != "_googlecast._tcp" || s.Instance != "Living Room TV" ||
		s.TXT["md"] != "Chromecast" || s.TXT["fn"] != "Living Room TV" {
		t.Fatalf("service = %+v", s)
	}
	if v, ok := s.TXT["rs"]; !ok || v != "" {
		t.Fatalf("bare key: %+v", s.TXT)
	}
}

func TestBrowseMDNSFollowsUpForTXT(t *testing.T) {
	r := newResponder(t, false,
		map[string]string{"_ipp._tcp.local.": "Brother HL-L2350DW._ipp._tcp.local."},
		map[string][]string{"Brother HL-L2350DW._ipp._tcp.local.": {"ty=Brother HL-L2350DW series", "usb_MFG=Brother"}})
	got, err := browseMDNS(context.Background(), nil, r.addr(), []string{"_ipp._tcp"}, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TXT["ty"] != "Brother HL-L2350DW series" || got[0].TXT["usb_mfg"] != "Brother" {
		t.Fatalf("services = %+v", got)
	}
	select {
	case name := <-r.txtAsked:
		if !strings.HasPrefix(name, "Brother HL-L2350DW.") {
			t.Fatalf("asked TXT for %q", name)
		}
	default:
		t.Fatal("no follow-up TXT question")
	}
}

func TestBrowseMDNSNothingAnswers(t *testing.T) {
	r := newResponder(t, true, map[string]string{}, map[string][]string{}) // knows nothing
	got, err := browseMDNS(context.Background(), nil, r.addr(), MDNSTypes, 200*time.Millisecond)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestInterfaceForLoopback(t *testing.T) {
	// Loopback is not multicast-capable on most systems, so nothing matches
	// 127.0.0.0/8; an unparsable CIDR is nil too.
	if InterfaceFor("not a cidr") != nil {
		t.Fatal("bad cidr matched")
	}
}

func TestBuildQuestionsSplits(t *testing.T) {
	qs, err := buildQuestions(MDNSTypes)
	if err != nil || len(qs) != 1 {
		t.Fatalf("default types: %d packets, %v", len(qs), err)
	}
	var many []string
	for i := 0; i < 100; i++ {
		many = append(many, "_"+strings.Repeat("x", 20)+string(rune('a'+i%26))+strings.Repeat("y", i%5)+"._tcp")
	}
	qs, err = buildQuestions(many)
	if err != nil || len(qs) < 2 {
		t.Fatalf("100 types: %d packets, %v", len(qs), err)
	}
	total := 0
	for _, q := range qs {
		if len(q) > maxQuery {
			t.Fatalf("packet of %d bytes", len(q))
		}
		var p dnsmessage.Parser
		if _, err := p.Start(q); err != nil {
			t.Fatal(err)
		}
		got, _ := p.AllQuestions()
		total += len(got)
	}
	if total != len(many) {
		t.Fatalf("questions = %d, want %d", total, len(many))
	}
}
