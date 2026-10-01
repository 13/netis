package leases

import (
	"context"
	"math/big"
	"net/netip"
	"strings"

	"netis/internal/store"
)

// Range is a DHCP pool as a DHCP server reports it: the first and last
// address it hands out, both inclusive.
type Range struct {
	Start string
	End   string
}

// ParseRange reads a pool written "first-last" or as a CIDR block, the two
// shapes DHCP servers use. A range whose ends are of different families or
// out of order is refused.
func ParseRange(s string) (Range, bool) {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		p = p.Masked()
		return Range{Start: p.Addr().String(), End: lastAddr(p).String()}, true
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return Range{}, false
	}
	r := Range{Start: NormIP(a), End: NormIP(b)}
	if _, _, ok := r.bounds(); !ok {
		return Range{}, false
	}
	return r, true
}

// bounds parses the range, reporting false unless both ends are addresses of
// the same family in order.
func (r Range) bounds() (lo, hi netip.Addr, ok bool) {
	lo, err1 := netip.ParseAddr(r.Start)
	hi, err2 := netip.ParseAddr(r.End)
	if err1 != nil || err2 != nil || lo.Is4() != hi.Is4() || hi.Less(lo) {
		return lo, hi, false
	}
	return lo, hi, true
}

// lastAddr is the highest address in p.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := range b {
		bits := p.Bits() - i*8
		switch {
		case bits <= 0:
			b[i] = 0xff
		case bits < 8:
			b[i] |= 0xff >> bits
		}
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// size is how many addresses the range holds, less one; only compared.
func size(lo, hi netip.Addr) *big.Int {
	l, h := lo.As16(), hi.As16()
	return new(big.Int).Sub(new(big.Int).SetBytes(h[:]), new(big.Int).SetBytes(l[:]))
}

// ApplyPools records the DHCP pools source read from its server. Each subnet
// takes the largest range lying wholly inside it (the most specific subnet,
// when subnets nest); ranges that fit no subnet or do not parse are skipped.
// A pool the user set, or another integration's, is left alone (see
// store.SetDHCPPoolFrom), and nothing is ever cleared: a pool that vanishes
// upstream stays until someone clears it. It returns how many subnets
// changed.
func ApplyPools(ctx context.Context, st *store.Store, source string, subnets []store.Subnet, ranges []Range) (int, error) {
	type pick struct {
		r    Range
		size *big.Int
	}
	best := map[int64]pick{}
	for _, r := range ranges {
		lo, hi, ok := r.bounds()
		if !ok {
			continue
		}
		var id int64
		bits := -1
		for _, sn := range subnets {
			p, err := netip.ParsePrefix(sn.CIDR)
			if err != nil || !p.Contains(lo) || !p.Contains(hi) || p.Bits() <= bits {
				continue
			}
			id, bits = sn.ID, p.Bits()
		}
		if bits < 0 {
			continue
		}
		n := size(lo, hi)
		if b, seen := best[id]; !seen || n.Cmp(b.size) > 0 {
			best[id] = pick{Range{Start: lo.String(), End: hi.String()}, n}
		}
	}
	changed := 0
	for id, b := range best {
		c, err := st.SetDHCPPoolFrom(ctx, id, b.r.Start, b.r.End, source)
		if err != nil {
			return changed, err
		}
		if c {
			changed++
		}
	}
	return changed, nil
}
