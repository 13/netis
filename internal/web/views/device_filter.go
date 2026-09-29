package views

import (
	"fmt"
	"net/url"
	"strconv"

	"netis/internal/store"
)

// DeviceFilter is the devices page's filter as it appears in the query
// string, so a filtered list is a link that can be shared, bookmarked, and
// opened without JavaScript: q (free text), status (online/offline), kind,
// subnet (an id), tag, and the flags new=1 (unreviewed), private=1 (a
// randomized MAC) and missing=1 (its integration no longer lists it).
type DeviceFilter struct {
	Q, Status, Kind, Tag     string
	Subnet                   int64
	New, PrivateMAC, Missing bool
}

// Values is the filter as query parameters, leaving out what is not set.
func (f DeviceFilter) Values() url.Values {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("q", f.Q)
	set("status", f.Status)
	set("kind", f.Kind)
	if f.Subnet > 0 {
		v.Set("subnet", strconv.FormatInt(f.Subnet, 10))
	}
	set("tag", f.Tag)
	flag := func(k string, on bool) {
		if on {
			v.Set(k, "1")
		}
	}
	flag("new", f.New)
	flag("private", f.PrivateMAC)
	flag("missing", f.Missing)
	return v
}

// Active reports whether anything narrows the list.
func (f DeviceFilter) Active() bool { return len(f.Values()) > 0 }

// Count is how many filters besides the text search are set, for the
// "Filters (2)" label of the folded filter panel on phones.
func (f DeviceFilter) Count() int {
	n := len(f.Values())
	if f.Q != "" {
		n--
	}
	return n
}

// DeviceListPage is everything the devices page shows.
type DeviceListPage struct {
	Rows   []store.DeviceRow
	Filter DeviceFilter
	// Sort and Dir are the sort in effect. Explicit is false when the reader
	// chose none: the default order (by IP) then nests guests under their
	// host. Any sort the reader picks shows a flat list in that order.
	Sort, Dir string
	Explicit  bool
	// Matched is how many devices passed the filter, before the page cap;
	// All is how many devices exist.
	Matched, All int
	Subnets      []store.Subnet
	Tags         []string
	// Conflicts holds the addresses (ConflictKey) more than one device
	// holds in the same subnet.
	Conflicts map[string]bool
}

// Grouped reports whether the table nests children under their parent.
func (p DeviceListPage) Grouped() bool { return !p.Explicit }

// ConflictKey identifies an address within its subnet.
func ConflictKey(subnetID int64, ip string) string {
	return fmt.Sprintf("%d|%s", subnetID, ip)
}

// deviceTableOpts configures the shared devices table: the devices page uses
// every option, the subnet page only the sort.
type deviceTableOpts struct {
	// Base is the page hosting the table; its sort links point there and
	// keep Keep (the page's other query parameters).
	Base string
	Keep url.Values
	// Sort and Dir mark the sorted column. An empty Sort marks none.
	Sort, Dir string
	Grouped   bool
	// Bulk adds the selection checkboxes (admins, devices page).
	Bulk      bool
	Conflicts map[string]bool
	// Next is where a row's Approve sends the browser back to.
	Next string
}

// sortLink is the header link for col: the page with its other parameters
// kept and col sorted, flipping the direction when col is already sorted.
func (o deviceTableOpts) sortLink(col string) string {
	v := url.Values{}
	for k, vals := range o.Keep {
		v[k] = vals
	}
	v.Set("sort", col)
	v.Set("dir", nextDir(col, o.Sort, o.Dir))
	return o.Base + "?" + v.Encode()
}

// ariaSort is the header's aria-sort value.
func (o deviceTableOpts) ariaSort(col string) string {
	switch {
	case o.Sort != col:
		return "none"
	case o.Dir == "desc":
		return "descending"
	}
	return "ascending"
}

func nextDir(col, curSort, curDir string) string {
	if curSort == col && curDir == "asc" {
		return "desc"
	}
	return "asc"
}

// kindLabels names device kinds for people; the stored value is a slug.
var kindLabels = map[string]string{
	"computer": "Computer", "switch": "Switch", "router": "Router", "modem": "Modem",
	"phone": "Phone", "server": "Server", "printer": "Printer", "iot": "IoT",
	"vm": "Virtual machine", "lxc": "Container", "wg-peer": "WireGuard peer", "other": "Other",
}

// KindLabel is a device kind as the UI shows it.
func KindLabel(kind string) string {
	if l, ok := kindLabels[kind]; ok {
		return l
	}
	return kind
}

// deviceCount is "1 device" or "31 devices".
func deviceCount(n int) string {
	if n == 1 {
		return "1 device"
	}
	return strconv.Itoa(n) + " devices"
}

// subnetLabel names a subnet in the filter menu: its name and CIDR.
func subnetLabel(sn store.Subnet) string {
	if sn.Name == "" {
		return sn.CIDR
	}
	return sn.Name + " · " + sn.CIDR
}
