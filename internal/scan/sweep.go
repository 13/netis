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

// AllIPs returns every address in cidr from the network address to the
// broadcast address inclusive.
func AllIPs(cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	var out []string
	for addr := prefix.Addr(); prefix.Contains(addr); addr = addr.Next() {
		out = append(out, addr.String())
	}
	return out, nil
}

// HostIPs returns the usable host addresses in cidr: the full range with the
// network and broadcast addresses trimmed for IPv4 subnets shorter than /31.
func HostIPs(cidr string) ([]string, error) {
	out, err := AllIPs(cidr)
	if err != nil {
		return nil, err
	}
	prefix, _ := netip.ParsePrefix(cidr) // already validated by AllIPs
	prefix = prefix.Masked()
	if prefix.Addr().Is4() && prefix.Bits() < 31 && len(out) >= 2 {
		out = out[1 : len(out)-1]
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

	concurrency := s.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case i, ok := <-jobs:
					if !ok {
						return
					}
					results[i] = s.ping(ctx, ips[i])
				}
			}
		}()
	}

feed:
	for i := range ips {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- i:
		}
	}
	close(jobs)
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
