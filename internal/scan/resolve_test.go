package scan

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestReverseName(t *testing.T) {
	if got := reverseName("192.168.1.42"); got != "42.1.168.192.in-addr.arpa." {
		t.Errorf("got %q", got)
	}
	if reverseName("fe80::1") != "" || reverseName("nope") != "" {
		t.Error("non-IPv4 must have no reverse name")
	}
}

func TestBuildMDNSQuery(t *testing.T) {
	msg, err := buildMDNSQuery("42.1.168.192.in-addr.arpa.")
	if err != nil {
		t.Fatal(err)
	}
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || h.Response {
		t.Fatalf("header=%+v err=%v", h, err)
	}
	q, err := p.Question()
	if err != nil {
		t.Fatal(err)
	}
	if q.Name.String() != "42.1.168.192.in-addr.arpa." || q.Type != dnsmessage.TypePTR {
		t.Fatalf("question=%+v", q)
	}
	if q.Class&qu == 0 || q.Class&^qu != dnsmessage.ClassINET {
		t.Fatalf("class=%#x, want IN with the unicast-response bit", uint16(q.Class))
	}
}

// mdnsReply builds a response to q answering with ptr for name, preceded by an
// unrelated record the parser has to skip.
func mdnsReply(t *testing.T, name, ptr string) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	other := dnsmessage.MustNewName("printer.local.")
	if err := b.AResource(dnsmessage.ResourceHeader{Name: other, Class: dnsmessage.ClassINET, TTL: 120},
		dnsmessage.AResource{A: [4]byte{10, 0, 0, 7}}); err != nil {
		t.Fatal(err)
	}
	if err := b.PTRResource(dnsmessage.ResourceHeader{
		Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET | 0x8000, TTL: 120,
	}, dnsmessage.PTRResource{PTR: dnsmessage.MustNewName(ptr)}); err != nil {
		t.Fatal(err)
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestParseMDNSAnswer(t *testing.T) {
	name := "42.1.168.192.in-addr.arpa."
	if got := parseMDNSAnswer(mdnsReply(t, name, "Bens-iPhone.local."), name); got != "Bens-iPhone.local" {
		t.Errorf("got %q", got)
	}
	if got := parseMDNSAnswer(mdnsReply(t, "1.1.168.192.in-addr.arpa.", "other.local."), name); got != "" {
		t.Errorf("an answer for another name must be ignored, got %q", got)
	}
	q, _ := buildMDNSQuery(name)
	if got := parseMDNSAnswer(q, name); got != "" {
		t.Errorf("a query is not an answer, got %q", got)
	}
	if got := parseMDNSAnswer([]byte{1, 2, 3}, name); got != "" {
		t.Errorf("garbage parsed as %q", got)
	}
}

// startResponder runs a UDP mDNS stand-in on loopback. For each query it
// sends a reply for a different name first, then the real answer, back to the
// querier's address, as a responder honouring the QU bit does.
func startResponder(t *testing.T, answer string) (string, *atomic.Int32) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	var got atomic.Int32
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			if _, err := p.Start(buf[:n]); err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil || q.Type != dnsmessage.TypePTR || q.Class&qu == 0 {
				continue
			}
			got.Add(1)
			if answer == "" {
				continue // silent host
			}
			conn.WriteToUDP(mdnsReply(t, "9.9.9.9.in-addr.arpa.", "noise.local."), from)
			conn.WriteToUDP(mdnsReply(t, q.Name.String(), answer), from)
		}
	}()
	return conn.LocalAddr().String(), &got
}

func TestMDNSLookupAgainstLocalResponder(t *testing.T) {
	addr, queries := startResponder(t, "livingroom-tv.local.")
	got := mdnsLookupAddr(t.Context(), "192.168.1.42", addr, time.Second)
	if got != "livingroom-tv.local" {
		t.Fatalf("got %q", got)
	}
	if queries.Load() != 1 {
		t.Fatalf("responder saw %d QU PTR queries, want 1", queries.Load())
	}
}

func TestMDNSLookupTimesOut(t *testing.T) {
	addr, _ := startResponder(t, "")
	start := time.Now()
	if got := mdnsLookupAddr(t.Context(), "192.168.1.42", addr, 100*time.Millisecond); got != "" {
		t.Fatalf("got %q from a silent responder", got)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("lookup ignored its timeout: %v", d)
	}
}

// TestResolveAllIsConcurrent checks lookups overlap, stay within the bound,
// and that an empty answer is not retried on the next sweep.
func TestResolveAllIsConcurrent(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var calls atomic.Int32
	e := &Engine{Resolve: func(ctx context.Context, ip string) string {
		calls.Add(1)
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		if strings.HasSuffix(ip, ".1") {
			return "gw.lan"
		}
		return ""
	}}
	var ips []string
	for i := 1; i <= 40; i++ {
		ips = append(ips, "10.0.0."+strconv.Itoa(i))
	}
	names := e.resolveAll(t.Context(), ips)
	if names["10.0.0.1"] != "gw.lan" {
		t.Fatalf("names=%v", names)
	}
	if peak < 2 || peak > resolveParallel {
		t.Fatalf("peak concurrency %d, want 2..%d", peak, resolveParallel)
	}
	before := calls.Load()
	e.resolveAll(t.Context(), ips)
	// Only the hosts that had names are asked again (they would not be in a
	// real sweep either, having a hostname by then); misses wait.
	if extra := calls.Load() - before; extra != 1 { // only 10.0.0.1
		t.Fatalf("second pass made %d lookups, want 1", extra)
	}
}
