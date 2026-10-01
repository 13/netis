package web

import (
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"netis/internal/macaddr"
	"netis/internal/store"
	"netis/internal/web/views"
)

// maxDeviceRows caps how many devices the list page renders at once. Well above
// any home LAN, low enough that a runaway scan cannot produce a page no browser
// will finish laying out. A var so tests can exercise the cap without creating
// two thousand devices.
var maxDeviceRows = 2000

// parseDeviceSort reads and normalizes the ?sort=&dir= query used by the
// devices table (whitelist ip/name/status/kind/seen, default ip; dir asc unless
// desc). Shared by the devices list and the subnet page.
func parseDeviceSort(r *http.Request) (sortKey, dir string) {
	sortKey = r.URL.Query().Get("sort")
	switch sortKey {
	case "ip", "name", "status", "kind", "seen":
	default:
		sortKey = "ip"
	}
	dir = r.URL.Query().Get("dir")
	if dir != "desc" {
		dir = "asc"
	}
	return sortKey, dir
}

func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	// A plain GET form sends every field, the empty ones too. Send the
	// browser to the same list without them, so the URL says only what is
	// filtered.
	if !isHTMX(r) && hasEmptyParams(r.URL.Query()) {
		v := r.URL.Query()
		for k, vals := range v {
			if len(vals) == 0 || vals[0] == "" {
				v.Del(k)
			}
		}
		target := "/devices"
		if len(v) > 0 {
			target += "?" + v.Encode()
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}

	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := views.DeviceListPage{All: len(rows), Subnets: subnets, Conflicts: ipConflicts(rows)}
	tagColors := make(views.TagColors, len(tags))
	for _, t := range tags {
		p.Tags = append(p.Tags, t.Name)
		tagColors[t.Name] = t.Color
	}
	p.TagColors = tagColors
	p.Filter = parseDeviceFilter(r)
	if p.Filter.Tag != "" {
		p.ClearTagHref = clearTagHref(r)
	}
	rows = filterDevices(rows, p.Filter.Q)
	rows = applyDeviceFilter(rows, p.Filter)

	p.Sort, p.Dir = parseDeviceSort(r)
	p.Explicit = r.URL.Query().Get("sort") != ""
	sortDeviceRows(rows, p.Sort, p.Dir)

	// Bound what goes into the page. The query behind this is a fixed five
	// statements whatever the fleet size, but the HTML is one row plus one grid
	// tile per device, and a page with tens of thousands of them is unusable
	// before it is slow. The filter is applied first, so narrowing it reaches
	// anything the cap cuts off.
	p.Matched = len(rows)
	if len(rows) > maxDeviceRows {
		rows = rows[:maxDeviceRows]
	}
	p.Rows = rows

	// The filter bar asks htmx for the results alone; it also carries the
	// active-tag chip and clear link as an out-of-band swap, since only
	// #dev-results itself gets replaced.
	if isHTMX(r) && r.Header.Get("HX-Target") == "dev-results" {
		s.render(w, r, views.DeviceResultsHTMX(p))
		return
	}
	u, _ := userFrom(r)
	s.render(w, r, views.DeviceList(u.Username, p))
}

// hasEmptyParams reports whether any query parameter is present but blank.
func hasEmptyParams(v url.Values) bool {
	for _, vals := range v {
		if len(vals) == 0 || vals[0] == "" {
			return true
		}
	}
	return false
}

// parseDeviceFilter reads the devices page filter from the query string.
// Values it does not know (a kind that does not exist, a subnet id that is
// not a number) are dropped rather than matching nothing.
func parseDeviceFilter(r *http.Request) views.DeviceFilter {
	q := r.URL.Query()
	f := views.DeviceFilter{
		Q:          strings.TrimSpace(q.Get("q")),
		Tag:        strings.TrimSpace(q.Get("tag")),
		New:        q.Get("new") == "1",
		PrivateMAC: q.Get("private") == "1",
		Missing:    q.Get("missing") == "1",
	}
	if st := q.Get("status"); st == "online" || st == "offline" {
		f.Status = st
	}
	if k := q.Get("kind"); validKinds[k] {
		f.Kind = k
	}
	if id, err := strconv.ParseInt(q.Get("subnet"), 10, 64); err == nil && id > 0 {
		f.Subnet = id
	}
	return f
}

// clearTagHref is the current request's URL with the tag filter removed and
// every other query parameter (search, subnet, sort, toggles) kept, for the
// clear link next to the tag filter select.
func clearTagHref(r *http.Request) string {
	v := r.URL.Query()
	v.Del("tag")
	if len(v) == 0 {
		return "/devices"
	}
	return "/devices?" + v.Encode()
}

// applyDeviceFilter keeps the rows that pass every filter set in f except the
// text search (filterDevices does that).
func applyDeviceFilter(rows []store.DeviceRow, f views.DeviceFilter) []store.DeviceRow {
	kept := rows[:0]
	for _, row := range rows {
		if deviceMatches(row, f) {
			kept = append(kept, row)
		}
	}
	return kept
}

func deviceMatches(row store.DeviceRow, f views.DeviceFilter) bool {
	switch {
	case f.Status == "online" && !row.Online,
		f.Status == "offline" && row.Online,
		f.Kind != "" && row.Kind != f.Kind,
		f.New && row.Reviewed,
		f.PrivateMAC && !macaddr.AnyPrivate(row.MACs),
		f.Missing && row.UpstreamMissingSince == nil:
		return false
	}
	if f.Subnet > 0 && !slices.ContainsFunc(row.IPs, func(ip store.IPInfo) bool { return ip.SubnetID == f.Subnet }) {
		return false
	}
	if f.Tag != "" && !slices.ContainsFunc(row.TagNames, func(t string) bool { return strings.EqualFold(t, f.Tag) }) {
		return false
	}
	return true
}

// ipConflicts finds the addresses more than one device holds in the same
// subnet, keyed by views.ConflictKey. It looks at every device, not only the
// ones a filter shows, so a conflict is flagged even when the other holder is
// filtered out.
func ipConflicts(rows []store.DeviceRow) map[string]bool {
	holders := map[string]int64{}
	out := map[string]bool{}
	for _, row := range rows {
		for _, ip := range row.IPs {
			k := views.ConflictKey(ip.SubnetID, ip.IP)
			if id, ok := holders[k]; ok && id != row.ID {
				out[k] = true
			} else if !ok {
				holders[k] = row.ID
			}
		}
	}
	return out
}

// filterDevices keeps the rows whose name, IPs, MACs, tags, vendor, model
// or function contain q, ignoring case. An empty q keeps everything. The
// device list filter and the command palette search both use it, so they
// find the same devices.
func filterDevices(rows []store.DeviceRow, q string) []store.DeviceRow {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		var ips []string
		for _, ip := range row.IPs {
			ips = append(ips, ip.IP)
		}
		hay := strings.ToLower(row.Name + " " + strings.Join(ips, " ") + " " +
			strings.Join(row.MACs, " ") + " " + strings.Join(row.TagNames, " ") + " " +
			row.Vendor + " " + row.Model + " " + row.Function)
		if strings.Contains(hay, q) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// lowestIP returns the device's numerically smallest IP, or the zero Addr
// (which sorts before all real addresses) when it has none; callers push
// no-IP devices to the end explicitly.
func lowestIP(row store.DeviceRow) (netip.Addr, bool) {
	var best netip.Addr
	found := false
	for _, ip := range row.IPs {
		a, err := netip.ParseAddr(ip.IP)
		if err != nil {
			continue
		}
		if !found || a.Compare(best) < 0 {
			best, found = a, true
		}
	}
	return best, found
}

func sortDeviceRows(rows []store.DeviceRow, key, dir string) {
	less := func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch key {
		case "name":
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case "kind":
			return a.Kind < b.Kind
		case "status":
			return a.Online && !b.Online // online first
		case "seen":
			as, bs := "", ""
			if a.LastSeen != nil {
				as = *a.LastSeen
			}
			if b.LastSeen != nil {
				bs = *b.LastSeen
			}
			return as > bs // most-recent first; never-seen ("") last
		default: // ip
			ai, aok := lowestIP(a)
			bi, bok := lowestIP(b)
			if aok != bok {
				return aok // devices with an IP sort before those without
			}
			if !aok {
				return false
			}
			return ai.Compare(bi) < 0
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if dir == "desc" {
			return less(j, i)
		}
		return less(i, j)
	})
}
