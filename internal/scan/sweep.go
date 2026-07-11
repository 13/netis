package scan

import (
	"context"
	"net/netip"
	"os"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

type Result struct {
	IP    string
	Alive bool
	RTTms float64
}

type Sweeper interface {
	Sweep(ctx context.Context, cidr string) ([]Result, error)
}

func HostIPs(cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	var out []string
	skipEdges := prefix.Addr().Is4() && prefix.Bits() < 31
	first := prefix.Addr()
	for addr := first; prefix.Contains(addr); addr = addr.Next() {
		out = append(out, addr.String())
	}
	if skipEdges && len(out) >= 2 {
		out = out[1 : len(out)-1] // drop network + broadcast
	}
	return out, nil
}

type ICMPSweeper struct {
	Concurrency int
	Timeout     time.Duration
}

func NewICMPSweeper(concurrency int) *ICMPSweeper {
	return &ICMPSweeper{Concurrency: concurrency, Timeout: time.Second}
}

func (s *ICMPSweeper) Sweep(ctx context.Context, cidr string) ([]Result, error) {
	ips, err := HostIPs(cidr)
	if err != nil {
		return nil, err
	}
	results := make([]Result, len(ips))
	sem := make(chan struct{}, s.Concurrency)
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = s.ping(ctx, ip)
		}(i, ip)
	}
	wg.Wait()
	return results, ctx.Err()
}

func (s *ICMPSweeper) ping(ctx context.Context, ip string) Result {
	p, err := probing.NewPinger(ip)
	if err != nil {
		return Result{IP: ip}
	}
	p.Count = 1
	p.Timeout = s.Timeout
	p.SetPrivileged(os.Getenv("NETIS_PRIVILEGED_ICMP") == "1")
	if err := p.RunWithContext(ctx); err != nil {
		return Result{IP: ip}
	}
	stats := p.Statistics()
	if stats.PacketsRecv == 0 {
		return Result{IP: ip}
	}
	return Result{IP: ip, Alive: true, RTTms: float64(stats.AvgRtt.Microseconds()) / 1000}
}
