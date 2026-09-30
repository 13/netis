package autofill

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"netis/internal/probe"
	"netis/internal/store"
)

// SubnetProber finds hints on one link, keyed by the answering address.
type SubnetProber func(ctx context.Context, ifi *net.Interface) (map[string][]store.Hint, error)

// Service is the scan engine's Autofiller.
var _ interface {
	Kick()
	Probe(store.Subnet)
} = (*Service)(nil)

// probeTimeout bounds one subnet's probes, all sources together.
const probeTimeout = 10 * time.Second

func mdnsProber(ctx context.Context, ifi *net.Interface) (map[string][]store.Hint, error) {
	svcs, err := probe.BrowseMDNS(ctx, ifi, probe.MDNSTypes, 3*time.Second)
	if err != nil {
		return nil, err
	}
	byIP := map[string][]probe.MDNSService{}
	for _, sv := range svcs {
		byIP[sv.IP] = append(byIP[sv.IP], sv)
	}
	out := make(map[string][]store.Hint, len(byIP))
	for ip, list := range byIP {
		if hs := mdnsHints(list); len(hs) > 0 {
			out[ip] = hs
		}
	}
	return out, nil
}

func ssdpProber(ctx context.Context, ifi *net.Interface) (map[string][]store.Hint, error) {
	devs, err := probe.SearchSSDP(ctx, ifi, 3*time.Second)
	if err != nil {
		return nil, err
	}
	byIP := map[string][]probe.UPnPDevice{}
	for _, d := range devs {
		byIP[d.IP] = append(byIP[d.IP], d)
	}
	out := make(map[string][]store.Hint, len(byIP))
	for ip, list := range byIP {
		if hs := ssdpHints(list); len(hs) > 0 {
			out[ip] = hs
		}
	}
	return out, nil
}

// Probe asks for sn to be probed soon. It never blocks; a full queue drops
// the request (the next sweep asks again).
func (s *Service) Probe(sn store.Subnet) {
	select {
	case s.probeCh <- sn:
	default:
	}
}

// probeLoop probes the subnets Probe asks for, until ctx is done. Only IPv4
// subnets a local interface is attached to are probed, each at most once
// per probeEvery.
func (s *Service) probeLoop(ctx context.Context) {
	for {
		var sn store.Subnet
		select {
		case <-ctx.Done():
			return
		case sn = <-s.probeCh:
		}
		if ctx.Err() != nil {
			return
		}
		if !Enabled(ctx, s.st) {
			continue
		}
		if last, ok := s.probedAt[sn.ID]; ok && s.now().Sub(last) < s.probeEvery {
			continue
		}
		// The probes speak IPv4 only.
		if p, err := netip.ParsePrefix(sn.CIDR); err != nil || !p.Addr().Is4() {
			continue
		}
		ifi := s.iface(sn.CIDR)
		if ifi == nil {
			continue
		}
		s.probedAt[sn.ID] = s.now()
		if err := s.probeSubnet(ctx, sn, ifi); err != nil && ctx.Err() == nil {
			slog.Error("autofill probe failed", "subnet", sn.CIDR, "err", err)
		}
	}
}

// probeSubnet runs every prober on ifi, stores what each found for the
// subnet's devices, and fills in the devices that answered. A device that
// did not answer keeps its previous probe hints.
func (s *Service) probeSubnet(ctx context.Context, sn store.Subnet, ifi *net.Interface) error {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		found = map[string]map[string][]store.Hint{}
	)
	for name, p := range s.probers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := p(pctx, ifi)
			if err != nil {
				slog.Warn("autofill prober failed", "source", name, "subnet", sn.CIDR, "err", err)
				return
			}
			mu.Lock()
			found[name] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	rows, err := s.st.ListSubnetIfaceIPs(ctx, sn.ID)
	if err != nil {
		return err
	}
	deviceByIP := map[string]int64{}
	for _, r := range rows {
		if _, ok := deviceByIP[r.IP]; !ok {
			deviceByIP[r.IP] = r.DeviceID
		}
	}

	touched := map[int64]bool{}
	var ids []int64
	for source, byIP := range found {
		for ip, hs := range byIP {
			id, ok := deviceByIP[ip]
			if !ok {
				continue
			}
			for i := range hs {
				hs[i].DeviceID, hs[i].Source = id, source
			}
			existing, err := s.st.ListHints(ctx, id)
			if err != nil {
				return err
			}
			var stored []store.Hint
			for _, h := range existing {
				if h.Source == source {
					stored = append(stored, h)
				}
			}
			if !hintsEqual(stored, hs) {
				now := s.now().UTC().Format(time.RFC3339)
				for i := range hs {
					hs[i].SeenAt = now
				}
				if err := s.st.ReplaceHints(ctx, id, source, hs); err != nil {
					return err
				}
			}
			if !touched[id] {
				touched[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.Run(ctx, ids...)
}
