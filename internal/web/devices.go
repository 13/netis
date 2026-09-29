package web

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"netis/internal/macaddr"
	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
	"netis/internal/wol"
)

// maxDeviceRows caps how many devices the list page renders at once. Well above
// any home LAN, low enough that a runaway scan cannot produce a page no browser
// will finish laying out. A var so tests can exercise the cap without creating
// two thousand devices.
var maxDeviceRows = 2000

var validKinds = map[string]bool{"computer": true, "switch": true, "phone": true,
	"server": true, "printer": true, "iot": true, "vm": true, "lxc": true,
	"wg-peer": true, "router": true, "modem": true, "other": true}

// normMAC parses a 48-bit MAC in any of the usual spellings (colons, dashes
// or Cisco dots) into the lowercase colon form the store matches on. ok is
// false for anything else, including the right length made of non-hex.
func normMAC(in string) (string, bool) {
	hw, err := net.ParseMAC(strings.TrimSpace(in))
	if err != nil || len(hw) != 6 {
		return "", false
	}
	return hw.String(), true
}

// parseTags splits a comma-separated tags field into trimmed, non-empty names.
func parseTags(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

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
	for _, t := range tags {
		p.Tags = append(p.Tags, t.Name)
	}
	p.Filter = parseDeviceFilter(r)
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

	// The filter bar asks htmx for the results alone.
	if isHTMX(r) && r.Header.Get("HX-Target") == "dev-results" {
		s.render(w, r, views.DeviceResults(p))
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

// isHTMX reports whether r came from htmx rather than a plain browser request.
func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// deviceFormLists loads the subnets and devices the device form offers as
// choices.
func (s *Server) deviceFormLists(r *http.Request, f *views.DeviceForm) error {
	var err error
	if f.Subnets, err = s.store.ListSubnets(r.Context()); err != nil {
		return err
	}
	f.AllDevices, err = s.store.ListDevices(r.Context())
	return err
}

// renderDeviceForm answers with the device form: the dialog alone for htmx, which swaps it into #modal, and a page of its own
// otherwise, so the New and Edit links work without JavaScript and a plain
// post that failed shows its error with the form. status is the code to
// send, 200 for a fresh form.
func (s *Server) renderDeviceForm(w http.ResponseWriter, r *http.Request, f views.DeviceForm, status int) {
	if err := s.deviceFormLists(r, &f); err != nil {
		s.fail(w, r, err)
		return
	}
	if f.SubnetID == 0 {
		f.SubnetID = subnetFor(f.Subnets, f.IP)
	}
	var c templ.Component
	switch {
	case !isHTMX(r):
		u, _ := userFrom(r)
		c = views.DeviceFormPage(u.Username, f)
	default:
		c = views.DeviceDialog(f)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, c)
}

// subnetFor returns the id of the subnet that contains ip, so a form given
// only an address comes up with its subnet chosen. When subnets nest, the
// most specific wins. Zero when ip is not an address or no subnet holds it.
func subnetFor(subnets []store.Subnet, ip string) int64 {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return 0
	}
	var best int64
	bits := -1
	for _, sn := range subnets {
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil || !p.Contains(addr) || p.Bits() <= bits {
			continue
		}
		best, bits = sn.ID, p.Bits()
	}
	return best
}

// submittedDeviceForm is the form as the user filled it in, to send back
// with an error.
func submittedDeviceForm(r *http.Request, d store.Device, isEdit bool) views.DeviceForm {
	f := views.DeviceForm{Device: d, IsEdit: isEdit,
		MAC: r.FormValue("mac"), IP: r.FormValue("ip")}
	f.SubnetID, _ = strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	for _, name := range parseTags(r.FormValue("tags")) {
		f.Tags = append(f.Tags, store.Tag{Name: name})
	}
	return f
}

// redirectAfterForm sends the browser on to url once a form has saved. A
// dialog posts through htmx, which would follow a plain redirect itself and
// swap the next page into the dialog, so it is told to navigate instead.
func redirectAfterForm(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (s *Server) handleDeviceForm(w http.ResponseWriter, r *http.Request) {
	f := views.DeviceForm{Device: store.Device{Kind: "computer"}, IP: r.URL.Query().Get("ip")}
	if v := r.URL.Query().Get("subnet"); v != "" {
		f.SubnetID, _ = strconv.ParseInt(v, 10, 64)
	}
	f.Prefilled = f.IP != ""
	s.renderDeviceForm(w, r, f, http.StatusOK)
}

func (s *Server) handleDeviceEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	tags, err := s.store.DeviceTags(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderDeviceForm(w, r, views.DeviceForm{Device: d, Tags: tags, IsEdit: true}, http.StatusOK)
}

func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	dev := store.Device{
		Name: r.FormValue("name"), Kind: r.FormValue("kind"), Notes: r.FormValue("notes"),
		Icon: r.FormValue("icon"), Source: "manual",
		Vendor: r.FormValue("vendor"), Model: r.FormValue("model"), Function: r.FormValue("function"),
	}
	if p := r.FormValue("parent_device_id"); p != "" {
		if pid, err := strconv.ParseInt(p, 10, 64); err == nil {
			dev.ParentDeviceID = &pid
		}
	}
	// A refused form comes back filled in as submitted, with the reason.
	refuse := func(status int, msg string) {
		f := submittedDeviceForm(r, dev, false)
		f.Error = msg
		s.renderDeviceForm(w, r, f, status)
	}
	auditNote(r).Target = "device " + dev.Name
	// A subnet_id that is missing or not a number is "not chosen" (zero).
	subnetID, _ := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	nd, msg, err := s.checkNewDevice(r.Context(), dev, r.FormValue("mac"), r.FormValue("ip"), subnetID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if msg != "" {
		refuse(http.StatusBadRequest, msg)
		return
	}
	devID, err := s.store.CreateDeviceWithIface(r.Context(), nd.dev, nd.mac, nd.subnetID, nd.ip, "static")
	if err != nil {
		if status, msg, ok := writeFailure(err, macTakenMsg, parentMissingMsg); ok {
			refuse(status, msg)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetDeviceTags(r.Context(), devID, parseTags(r.FormValue("tags"))); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Device created")
	redirectAfterForm(w, r, "/devices/"+strconv.FormatInt(devID, 10))
}

const (
	macTakenMsg      = "that MAC address already belongs to another device"
	parentMissingMsg = "parent device does not exist"
	chooseSubnetMsg  = "choose the subnet the IP belongs to"
)

// newDevice is a validated create request, ready for CreateDeviceWithIface.
type newDevice struct {
	dev      store.Device
	mac      *string
	subnetID int64
	ip       string
}

// checkNewDevice validates a device create request, from the HTML form or the
// JSON API, so both refuse the same things with the same words. mac and ip
// are as submitted (blank for none); subnetID is zero when none was chosen.
// msg says what is wrong with the request; err is a failure to look the
// subnet up.
func (s *Server) checkNewDevice(ctx context.Context, dev store.Device, mac, ip string, subnetID int64) (nd newDevice, msg string, err error) {
	dev.Name = strings.TrimSpace(dev.Name)
	if !validKinds[dev.Kind] {
		return nd, "bad kind", nil
	}
	if dev.Name == "" {
		return nd, "name required", nil
	}
	nd.dev = dev
	if raw := strings.TrimSpace(mac); raw != "" {
		m, ok := normMAC(raw)
		if !ok {
			return nd, "invalid MAC address", nil
		}
		nd.mac = &m
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nd, "", nil
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nd, "invalid IP address", nil
	}
	if subnetID == 0 {
		return nd, chooseSubnetMsg, nil
	}
	sn, err := s.store.GetSubnet(ctx, subnetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nd, "unknown subnet", nil
	}
	if err != nil {
		return nd, "", err
	}
	prefix, err := netip.ParsePrefix(sn.CIDR)
	if err != nil {
		return nd, "", err
	}
	if !prefix.Contains(addr) {
		return nd, "IP " + addr.String() + " is not in subnet " + sn.CIDR, nil
	}
	nd.subnetID, nd.ip = sn.ID, addr.String()
	return nd, "", nil
}

func (s *Server) handleDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	auditNote(r).Target = deviceTarget(d)
	if kind := r.FormValue("kind"); validKinds[kind] {
		d.Kind = kind
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		d.Name = name
	}
	d.Notes = r.FormValue("notes")
	d.Icon = r.FormValue("icon")
	d.Vendor = r.FormValue("vendor")
	d.Model = r.FormValue("model")
	d.Function = r.FormValue("function")
	if p := r.FormValue("parent_device_id"); p != "" {
		if pid, err := strconv.ParseInt(p, 10, 64); err == nil && pid != d.ID {
			d.ParentDeviceID = &pid
		}
	} else {
		d.ParentDeviceID = nil
	}
	if err := s.store.UpdateDevice(r.Context(), d); err != nil {
		if status, msg, ok := writeFailure(err, "device conflicts with an existing one", parentMissingMsg); ok {
			f := submittedDeviceForm(r, d, true)
			f.Error = msg
			s.renderDeviceForm(w, r, f, status)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetDeviceTags(r.Context(), d.ID, parseTags(r.FormValue("tags"))); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Changes saved")
	redirectAfterForm(w, r, "/devices/"+r.PathValue("id"))
}

func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetDeviceReviewed(r.Context(), id, true); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Device approved")
	http.Redirect(w, r, localNext(r.FormValue("next"), "/devices"), http.StatusSeeOther)
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Name it in the audit log while there is still a device to name.
	if d, err := s.store.GetDevice(r.Context(), id); err == nil {
		auditNote(r).Target = deviceTarget(d)
	}
	if err := s.store.DeleteDevice(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Device deleted")
	http.Redirect(w, r, "/devices", http.StatusSeeOther)
}

// handleDevicePage assembles the full device detail view: fields, per-iface
// IPs/ports/availability, the 30-day availability bar, tags, custom fields,
// links, parent/children and recent event history.
func (s *Server) handleDevicePage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	ifaces, err := s.store.ListIfaces(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := time.Now()
	since := now.UTC().Add(-30 * 24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	ifaceDetails := make([]views.IfaceDetail, 0, len(ifaces))
	for _, f := range ifaces {
		ips, err := s.store.ListIPs(r.Context(), f.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		ports, err := s.store.ListOpenPorts(r.Context(), f.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		pct, err := s.store.AvailabilityPct(r.Context(), f.ID, since)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		online, lastSeen, err := s.store.IfaceOnline(r.Context(), f.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		ls := ""
		if lastSeen != nil {
			ls = *lastSeen
		}
		ifaceDetails = append(ifaceDetails, views.IfaceDetail{
			Iface: f, IPs: ips, Ports: ports, AvailabilityPct: pct, Online: online, LastSeen: ls,
		})
	}
	// The bar covers the local days ending today, so it asks from the start
	// of the first of them.
	y, m, day := now.Date()
	barSince := time.Date(y, m, day-views.AvailabilityDays+1, 0, 0, 0, 0, now.Location()).UTC().Format(time.RFC3339)
	hours, err := s.store.DeviceAvailability(r.Context(), id, barSince)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	subnetByID := make(map[int64]store.Subnet, len(subnets))
	for _, sn := range subnets {
		subnetByID[sn.ID] = sn
	}

	tags, err := s.store.DeviceTags(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	fields, err := s.store.ListCustomFields(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	links, err := s.store.ListLinks(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Parent and children come from the device rows, which carry the
	// status and IPs the page shows beside each of them. The query behind
	// ListDevices is a fixed handful of statements whatever the fleet size.
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var parent *store.DeviceRow
	var children []store.DeviceRow
	for i := range rows {
		row := rows[i]
		if d.ParentDeviceID != nil && row.ID == *d.ParentDeviceID {
			parent = &row
		}
		if row.ParentDeviceID != nil && *row.ParentDeviceID == id && row.ID != id {
			children = append(children, row)
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})
	evs, err := s.store.ListDeviceEvents(r.Context(), id, views.DeviceEventLimit)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	_, alert, err := s.store.DeviceAlert(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	u, _ := userFrom(r)
	s.render(w, r, views.DevicePage(u.Username, views.DeviceDetail{
		Device: d, Ifaces: ifaceDetails, Tags: tags,
		Fields: fields, Links: links, Children: children, Parent: parent, Events: evs,
		Subnets:      subnetByID,
		Availability: views.BuildAvailability(hours, now, views.AvailabilityDays),
		AlertOffline: alert,
		Back:         s.deviceBackLink(r),
	}))
}

// deviceBackLink points the device page's back link at the page the user
// came from, when the Referer is one of ours that lists devices: the list
// (with its filter and sort), the dashboard, events, or a subnet. Anything
// else, including a device page itself after a form post, goes to the list.
func (s *Server) deviceBackLink(r *http.Request) views.BackLink {
	list := views.BackLink{URL: "/devices", Label: "Devices"}
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Host != r.Host {
		return list
	}
	switch p := ref.Path; {
	case p == "/devices":
		if ref.RawQuery != "" {
			list.URL += "?" + ref.RawQuery
		}
		return list
	case p == "/":
		return views.BackLink{URL: "/", Label: "Dashboard"}
	case p == "/events":
		return views.BackLink{URL: "/events", Label: "Events"}
	case p == "/subnets":
		return views.BackLink{URL: "/subnets", Label: "Subnets"}
	case strings.HasPrefix(p, "/subnets/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(p, "/subnets/"), 10, 64)
		if err != nil {
			return list
		}
		sn, err := s.store.GetSubnet(r.Context(), id)
		if err != nil {
			return list
		}
		return views.BackLink{URL: "/subnets/" + strconv.FormatInt(id, 10), Label: sn.Name}
	}
	return list
}

// handleDeviceAlert switches the device's offline/online notifications on
// (alert_offline=1) or off.
func (s *Server) handleDeviceAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, _, err := s.store.DeviceAlert(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	on := r.FormValue("alert_offline") == "1"
	if err := s.store.SetDeviceAlertOffline(r.Context(), id, on); err != nil {
		s.fail(w, r, err)
		return
	}
	if on {
		s.flashToast(w, r, "Offline alerts turned on")
	} else {
		s.flashToast(w, r, "Offline alerts turned off")
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleLinkAdd(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	label := strings.TrimSpace(r.FormValue("label"))
	url := strings.TrimSpace(r.FormValue("url"))
	if label != "" && url != "" {
		if _, err := s.store.AddLink(r.Context(), id, label, url); err != nil {
			s.fail(w, r, err)
			return
		}
		s.flashToast(w, r, "Link added")
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleLinkDelete(w http.ResponseWriter, r *http.Request) {
	linkID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	// The device id isn't in this path (/links/{id}/delete), so redirect
	// target comes from the form's referring device id.
	devID := r.FormValue("device_id")
	if err := s.store.DeleteLink(r.Context(), linkID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Link removed")
	http.Redirect(w, r, "/devices/"+devID, http.StatusSeeOther)
}

func (s *Server) handleFieldSet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	key := strings.TrimSpace(r.FormValue("key"))
	if key != "" {
		if err := s.store.SetCustomField(r.Context(), id, key, r.FormValue("value")); err != nil {
			s.fail(w, r, err)
			return
		}
		s.flashToast(w, r, "Field saved")
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleFieldDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	key := r.FormValue("key")
	if err := s.store.DeleteCustomField(r.Context(), id, key); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Field removed")
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

// handleWOL sends a Wake-on-LAN magic packet to the MAC of the device's
// first interface that has one. It goes to the directed broadcast of every
// subnet that interface has an address in, so a host on another VLAN is
// reached, and to 255.255.255.255 as well. An htmx request is answered with a
// toast naming the addresses used; a plain form post redirects back to the
// device page.
func (s *Server) handleWOL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	ifaces, err := s.store.ListIfaces(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, f := range ifaces {
		if f.MAC == nil || *f.MAC == "" {
			continue
		}
		targets, err := s.wolTargets(r.Context(), f.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		sent, err := wol.SendAll(*f.MAC, targets, s.wolSend)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if isHTMX(r) {
			s.render(w, r, views.ScanToast("Wake on LAN sent to "+*f.MAC+" via "+wol.HostsOf(sent)))
			return
		}
		s.flashToast(w, r, "Wake on LAN sent to "+*f.MAC)
		http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
		return
	}
	http.Error(w, "this device has no MAC address, so it cannot be woken", http.StatusBadRequest)
}

// wolTargets returns where a magic packet for an interface goes: the directed
// broadcast of the subnet of each of its IPs (the subnet's prefix defines the
// broadcast, not the address), then the limited broadcast.
func (s *Server) wolTargets(ctx context.Context, ifaceID int64) ([]string, error) {
	ips, err := s.store.ListIPs(ctx, ifaceID)
	if err != nil {
		return nil, err
	}
	var prefixes []netip.Prefix
	for _, ip := range ips {
		sn, err := s.store.GetSubnet(ctx, ip.SubnetID)
		if err != nil {
			return nil, err
		}
		if p, err := netip.ParsePrefix(sn.CIDR); err == nil {
			prefixes = append(prefixes, p)
		}
	}
	return wol.Targets(prefixes), nil
}

// handlePortScan runs an on-demand TCP port scan against the first IP of
// the device's first interface, records the open ports it found in place of
// the previous result, and redirects back to the device page.
func (s *Server) handlePortScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	ifaces, err := s.store.ListIfaces(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(ifaces) == 0 {
		http.Error(w, "device has no interface", 400)
		return
	}
	ips, err := s.store.ListIPs(r.Context(), ifaces[0].ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(ips) == 0 {
		http.Error(w, "device has no IP", 400)
		return
	}
	open := scan.PortScan(r.Context(), ips[0].IP, scan.CommonPorts, time.Second)
	found := make([]store.OpenPort, 0, len(open))
	for _, p := range open {
		found = append(found, store.OpenPort{Port: p, ServiceGuess: scan.ServiceGuess(p)})
	}
	// The scan is the whole truth for the ports it probes, so it replaces the
	// recorded set: a port that has closed since the last scan disappears.
	if err := s.store.ReplaceOpenPorts(r.Context(), ifaces[0].ID, "tcp", found,
		time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, portScanToast(ips[0].IP, len(found)))
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

// portScanToast says what a port scan found, repeating what was done.
func portScanToast(ip string, open int) string {
	if open == 0 {
		return "Ports scanned on " + ip + ": none open"
	}
	return "Ports scanned on " + ip + ": " + strconv.Itoa(open) + " open"
}

// handleDeviceIPKind flips an IP assignment's lease kind (static/dhcp) from the
// device detail page and returns the re-rendered toggle control.
func (s *Server) handleDeviceIPKind(w http.ResponseWriter, r *http.Request) {
	devID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	subnetID, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad subnet", 400)
		return
	}
	ip := r.FormValue("ip")
	kind := r.FormValue("kind")
	if kind != "static" && kind != "dhcp" {
		http.Error(w, "bad kind", 400)
		return
	}
	if err := s.store.SetIPKind(r.Context(), subnetID, ip, kind); err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.LeaseToggle(devID, subnetID, ip, kind))
}
