package scan

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Presence fallback for hosts that drop ICMP. See
// docs/superpowers/specs/2026-09-28-netis-discovery-design.md for the reasoning
// behind the ports, the timeouts and the ARP settle wait.

// PresencePorts are the TCP ports probed to confirm a host that answered ARP
// but not ping: ssh, http, https, smb, iOS lockdownd and alt-http. Between
// them they cover most servers, Windows machines, iPhones and IoT web UIs.
var PresencePorts = []int{22, 80, 443, 445, 62078, 8080}

// presenceTimeout bounds a TCP presence probe. All ports are dialled at once,
// so this is the whole cost per host.
const presenceTimeout = 400 * time.Millisecond

// presenceParallel bounds how many hosts are TCP-probed at the same time.
const presenceParallel = 16

// DefaultARPSettle is how long after a sweep an ARP entry must still be
// COMPLETE to count on its own. Pinging a STALE neighbour starts the kernel's
// re-probe: 5s in DELAY (delay_first_probe_time), then 3 unicast ARP requests
// 1s apart (ucast_solicit, retrans_time) before the entry FAILs. 9s is past
// that with a margin, so an entry that is still COMPLETE was re-confirmed.
const DefaultARPSettle = 9 * time.Second

// TCPProbe reports whether ip accepts or refuses a TCP connection on any of
// PresencePorts, with the connect time in milliseconds. A refusal (RST) is as
// good as a handshake: only a live host sends one.
func TCPProbe(ctx context.Context, ip string) (float64, bool) {
	return tcpProbe(ctx, ip, PresencePorts, presenceTimeout)
}

func tcpProbe(ctx context.Context, ip string, ports []int, timeout time.Duration) (float64, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type res struct {
		rtt float64
		ok  bool
	}
	// Buffered so dials still in flight after an early return do not block.
	ch := make(chan res, len(ports))
	start := time.Now()
	var d net.Dialer
	for _, p := range ports {
		go func(p int) {
			c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(p)))
			rtt := float64(time.Since(start).Microseconds()) / 1000
			if err == nil {
				c.Close()
				ch <- res{rtt, true}
				return
			}
			ch <- res{rtt, errors.Is(err, syscall.ECONNREFUSED)}
		}(p)
	}
	for range ports {
		if r := <-ch; r.ok {
			return r.rtt, true
		}
	}
	return 0, false
}

// presenceEnabled reads the presence_fallback setting on every sweep. Anything
// but an explicit "off" leaves it on, the default.
func (e *Engine) presenceEnabled(ctx context.Context) bool {
	v, err := e.Store.GetSetting(ctx, "presence_fallback")
	if err != nil {
		slog.Error("reading presence_fallback setting failed", "err", err)
		return true
	}
	return v != "off"
}

// confirmPresence returns the addresses that missed ICMP but are present on
// the link, with an RTT for each. Candidates are addresses with a COMPLETE ARP
// entry after the sweep; routed subnets have none, so this is a no-op there.
// A candidate is confirmed by a TCP connect or refusal, or failing that by its
// ARP entry still resolving to the same MAC once the kernel has had time to
// re-probe it (ARPSettle after sweptAt).
func (e *Engine) confirmPresence(ctx context.Context, results []Result, arp map[string]string, sweptAt time.Time) map[string]float64 {
	var cands []string
	for _, r := range results {
		if r.Err == nil && !r.Alive && arp[r.IP] != "" {
			cands = append(cands, r.IP)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	confirmed := make(map[string]float64)
	if e.Presence != nil {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, presenceParallel)
		for _, ip := range cands {
			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()
				if rtt, ok := e.Presence(ctx, ip); ok {
					mu.Lock()
					confirmed[ip] = rtt
					mu.Unlock()
				}
			}(ip)
		}
		wg.Wait()
	}
	var rest []string
	for _, ip := range cands {
		if _, ok := confirmed[ip]; !ok {
			rest = append(rest, ip)
		}
	}
	if len(rest) == 0 {
		return confirmed
	}
	if wait := e.ARPSettle - time.Since(sweptAt); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return confirmed
		case <-t.C:
		}
	}
	again, err := e.ARP()
	if err != nil {
		return confirmed
	}
	for _, ip := range rest {
		if again[ip] != "" && again[ip] == arp[ip] {
			confirmed[ip] = 0
		}
	}
	return confirmed
}
