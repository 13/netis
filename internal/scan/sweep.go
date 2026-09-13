package scan

import (
	"context"
	"errors"
	"fmt"
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

// MaxSubnetAddresses caps how large a subnet netis will enumerate: 65536, an
// IPv4 /16 or an IPv6 /112.
//
// Every address in a subnet becomes a string here and a cell in the grid, so
// the cost is linear and the ceiling has to be real: a /8 is 16.7 million
// addresses and over a gigabyte before the page is rendered, and an IPv6
// prefix shorter than about /104 would never finish enumerating at all.
const MaxSubnetAddresses = 65536

// ErrSubnetTooLarge is returned for a prefix wider than MaxSubnetAddresses.
var ErrSubnetTooLarge = errors.New("subnet too large")

// SubnetTooLargeError describes a prefix netis refuses to enumerate.
type SubnetTooLargeError struct {
	CIDR string
	Bits int // the narrowest prefix length that would be accepted
}

func (e *SubnetTooLargeError) Error() string {
	return fmt.Sprintf("subnet %s is larger than the %d-address limit; use /%d or narrower",
		e.CIDR, MaxSubnetAddresses, e.Bits)
}

func (e *SubnetTooLargeError) Is(target error) bool { return target == ErrSubnetTooLarge }

// CheckSubnetSize reports whether cidr is small enough to enumerate. It is the
// cheap check: it compares prefix lengths and never walks the range, so it is
// safe to call on input that has not been validated yet.
func CheckSubnetSize(cidr string) error {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return err
	}
	// MaxSubnetAddresses is 2^16, so any prefix leaving more than 16 host bits
	// is over the limit.
	total := prefix.Addr().BitLen()
	if total-prefix.Bits() > 16 {
		return &SubnetTooLargeError{CIDR: cidr, Bits: total - 16}
	}
	return nil
}

// AllIPs returns every address in cidr from the network address to the
// broadcast address inclusive, refusing prefixes wider than
// MaxSubnetAddresses.
//
// The size check is repeated here rather than left to the caller on purpose:
// this is the function that does the allocating, subnets are stored in the
// database and may predate the limit, and an unbounded IPv6 prefix would hang
// the process rather than fail.
func AllIPs(cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}
	if err := CheckSubnetSize(cidr); err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	out := make([]string, 0, 256)
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
