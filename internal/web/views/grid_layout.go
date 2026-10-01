package views

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"

	"netis/internal/store"
)

// portsPerRow is how many ports one strip of the patch panel holds, and
// rowsPerSection how many strips make one panel: a /24 is exactly one panel
// of 16 x 16, and a larger subnet is a stack of them.
const (
	portsPerRow    = 16
	rowsPerSection = 16
)

// GridStats counts a subnet's addresses by what the grid shows for them.
// Static and DHCP split the held addresses by lease kind.
type GridStats struct {
	Online   int
	Offline  int
	Unseen   int
	Conflict int
	Free     int
	Pool     int // free, but in the DHCP pool
	Static   int
	DHCP     int
}

// FreeRange is a run of consecutive addresses free for static use.
type FreeRange struct {
	Start, End string
	N          int
}

// freeRangesShown caps the free ranges the subnet header lists; the rest
// are summed up in one "more" note.
const freeRangesShown = 6

// shownRanges is the free ranges the header lists, largest first so the
// room for a block of static addresses shows at once, then in address order.
func shownRanges(rs []FreeRange) []FreeRange {
	out := append([]FreeRange(nil), rs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].N > out[j].N })
	if len(out) > freeRangesShown {
		out = out[:freeRangesShown]
	}
	sort.SliceStable(out, func(i, j int) bool { return indexOf(rs, out[i]) < indexOf(rs, out[j]) })
	return out
}

func indexOf(rs []FreeRange, r FreeRange) int {
	for i, x := range rs {
		if x.Start == r.Start {
			return i
		}
	}
	return -1
}

// rangeLabel writes a free range compactly: ".50–.99" in an IPv4 subnet of
// a /24 or smaller, the full addresses otherwise, one address alone.
func rangeLabel(r FreeRange) string {
	short := func(ip string) string {
		a, err := netip.ParseAddr(ip)
		if err != nil || !a.Is4() {
			return ip
		}
		return fmt.Sprintf(".%d", a.As4()[3])
	}
	a, _ := netip.ParseAddr(r.Start)
	b, _ := netip.ParseAddr(r.End)
	if a.Is4() && b.Is4() && a.As4()[2] == b.As4()[2] && a.As4()[1] == b.As4()[1] {
		if r.N == 1 {
			return short(r.Start)
		}
		return short(r.Start) + "–" + short(r.End)
	}
	if r.N == 1 {
		return r.Start
	}
	return r.Start + " – " + r.End
}

// rangeAria names a free range for screen readers.
func rangeAria(r FreeRange) string {
	if r.N == 1 {
		return r.Start + ", 1 free address"
	}
	return fmt.Sprintf("%s to %s, %d free addresses", r.Start, r.End, r.N)
}

// portCard is what the hover card shows for a held address, keyed by the
// address in the grid's JSON: device name, MAC, when it was last seen and
// how many interfaces claim it.
type portCard struct {
	Name   string `json:"n"`
	MAC    string `json:"m,omitempty"`
	Seen   string `json:"s,omitempty"`
	Claims int    `json:"c,omitempty"`
}

// portCards gathers the hover card details of every held address. Free
// addresses need none: their port's state says it all.
func portCards(cells []GridCell) map[string]portCard {
	out := make(map[string]portCard)
	for _, c := range cells {
		if c.DeviceID == 0 {
			continue
		}
		pc := portCard{Name: c.Name, MAC: c.MAC, Seen: c.Seen}
		if c.Claims > 1 {
			pc.Claims = c.Claims
		}
		out[c.IP] = pc
	}
	return out
}

// ScanState is where scanning of one subnet stands, for its scan control.
// LastDone is RFC3339, empty when no scan has finished since netis started.
type ScanState struct {
	Subnet   store.Subnet
	Queued   bool
	Running  bool
	LastDone string
	LastOK   bool
}

// Busy reports whether a scan is waiting or under way.
func (s ScanState) Busy() bool { return s.Queued || s.Running }

// CellInfo is what the details panel shows for one address: its grid state,
// every interface that claims it, and for the network and broadcast
// addresses which of the two it is.
type CellInfo struct {
	IP     string
	State  string
	Edge   string // "network address" or "broadcast address"
	Claims []store.IPClaim
}

// GridPageData is everything the subnet page draws. Selected is the address
// whose details are open (a ?ip= link, or a click without JavaScript), and
// Cell its details.
type GridPageData struct {
	Username string
	Subnet   store.Subnet
	Cells    []GridCell
	Stats    GridStats
	NextFree string
	Free     []FreeRange
	Scan     ScanState
	Devices  []store.DeviceRow
	SortKey  string
	Dir      string
	Selected string
	Cell     *CellInfo
}

// SubnetSummary is one subnet on the subnets index. Cells is filled only for
// subnets small enough to draw as a miniature panel (a /24 or smaller);
// Hosts is the number of assignable addresses, for the bar drawn instead.
type SubnetSummary struct {
	Subnet store.Subnet
	Stats  GridStats
	Hosts  int
	Cells  []GridCell
	Scan   ScanState
}

// MiniPanelMax is the largest subnet the index draws as a miniature panel.
const MiniPanelMax = portsPerRow * rowsPerSection

// usageSegment is one coloured stretch of the usage bar, in percent.
type usageSegment struct {
	Class string
	X, W  float64
}

// usageSegments splits the usage bar of a subnet too large for a miniature
// panel into online, not seen yet, offline and conflict stretches.
func usageSegments(s SubnetSummary) []usageSegment {
	if s.Hosts <= 0 {
		return nil
	}
	var out []usageSegment
	x := 0.0
	for _, p := range []struct {
		class string
		n     int
	}{{"online", s.Stats.Online}, {"reserved", s.Stats.Unseen}, {"offline", s.Stats.Offline}, {"conflict", s.Stats.Conflict}} {
		w := float64(p.n) * 100 / float64(s.Hosts)
		if w > 0 {
			out = append(out, usageSegment{Class: p.class, X: x, W: w})
		}
		x += w
	}
	return out
}

// percent formats a percentage for an SVG attribute.
func percent(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64) + "%"
}

// PortRow is one strip of 16 ports, labelled with the address it starts at.
type PortRow struct {
	Label string
	Cells []GridCell
}

// PortSection is one panel of up to 16 strips. Label names its address range
// when the subnet needs more than one panel, and is empty otherwise.
type PortSection struct {
	Label string
	Rows  []PortRow
}

// portSections lays the cells out as the patch panel: rows of 16, stacked in
// panels of 16 rows. Rows of a /24 or smaller are labelled with the last
// octet (".16"), rows of a larger subnet with the last two ("4.16") so every
// row still says where it is.
func portSections(cells []GridCell) []PortSection {
	wide := len(cells) > portsPerRow*rowsPerSection
	var out []PortSection
	for start := 0; start < len(cells); start += portsPerRow * rowsPerSection {
		end := min(start+portsPerRow*rowsPerSection, len(cells))
		sec := PortSection{}
		if wide {
			sec.Label = cells[start].IP + " – " + cells[end-1].IP
		}
		for r := start; r < end; r += portsPerRow {
			re := min(r+portsPerRow, end)
			sec.Rows = append(sec.Rows, PortRow{Label: portRowLabel(cells[r].IP, wide), Cells: cells[r:re]})
		}
		out = append(out, sec)
	}
	return out
}

// portColumns is how many ports the widest row holds: 16, or fewer for a
// subnet smaller than a /28.
func portColumns(cells []GridCell) int {
	return min(len(cells), portsPerRow)
}

// portRowLabel is the edge label of a row starting at ip.
func portRowLabel(ip string, wide bool) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	if a.Is4() {
		b := a.As4()
		if wide {
			return fmt.Sprintf("%d.%d", b[2], b[3])
		}
		return fmt.Sprintf(".%d", b[3])
	}
	b := a.As16()
	return fmt.Sprintf(":%x", uint16(b[14])<<8|uint16(b[15]))
}

// cellWords names a grid state for people, capitalised for the details
// panel's status line.
var cellWords = map[string]string{
	"online": "Online", "offline": "Offline", "reserved": "Not seen yet",
	"conflict": "IP conflict", "free": "Free", "pool": "Free, in DHCP pool", "edge": "Not assignable",
}

// cellTone is the status tone class for a grid state.
func cellTone(state string) string {
	switch state {
	case "online":
		return "is-online"
	case "reserved":
		return "is-reserved"
	case "conflict":
		return "is-conflict"
	case "offline":
		return "is-offline"
	}
	return ""
}

// subnetKindLabels names subnet kinds for people.
var subnetKindLabels = map[string]string{
	"lan": "LAN", "wireguard": "WireGuard", "proxmox-bridge": "Proxmox bridge",
}

func subnetKindLabel(kind string) string {
	if l, ok := subnetKindLabels[kind]; ok {
		return l
	}
	return kind
}

// leaseLabel names a lease kind for people.
func leaseLabel(kind string) string {
	switch kind {
	case "static":
		return "Static"
	case "dhcp":
		return "DHCP"
	}
	return kind
}

// miniPort is one dot of a miniature panel on the subnets index: its
// position in the 16-wide drawing and its state class.
type miniPort struct {
	X, Y  int
	Class string
}

// miniPitch is the distance between miniature ports, in SVG units (a 4-unit
// dot and a 1-unit gap).
const miniPitch = 5

func miniPorts(cells []GridCell) []miniPort {
	out := make([]miniPort, len(cells))
	for i, c := range cells {
		out[i] = miniPort{X: (i % portsPerRow) * miniPitch, Y: (i / portsPerRow) * miniPitch, Class: c.State}
	}
	return out
}

// miniViewBox sizes a miniature panel's drawing to its cells.
func miniViewBox(cells []GridCell) string {
	rows := (len(cells) + portsPerRow - 1) / portsPerRow
	return fmt.Sprintf("0 0 %d %d", portColumns(cells)*miniPitch-1, rows*miniPitch-1)
}
