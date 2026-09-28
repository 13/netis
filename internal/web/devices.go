package web

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

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
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q != "" {
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
		rows = filtered
	}

	sortKey, dir := parseDeviceSort(r)
	sortDeviceRows(rows, sortKey, dir)

	// Bound what goes into the page. The query behind this is a fixed five
	// statements whatever the fleet size, but the HTML is one row plus one grid
	// tile per device, and a page with tens of thousands of them is unusable
	// before it is slow. The filter is applied first, so narrowing it reaches
	// anything the cap cuts off.
	total := len(rows)
	if len(rows) > maxDeviceRows {
		rows = rows[:maxDeviceRows]
	}

	u, _ := userFrom(r)
	s.render(w, r, views.DeviceList(u.Username, rows, r.URL.Query().Get("q"), sortKey, dir, total))
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

// renderDeviceForm answers with the device form: the dialog (or edit drawer)
// alone for htmx, which swaps it into #modal, and a page of its own
// otherwise, so the New and Edit links work without JavaScript and a plain
// post that failed shows its error with the form. status is the code to
// send, 200 for a fresh form.
func (s *Server) renderDeviceForm(w http.ResponseWriter, r *http.Request, f views.DeviceForm, status int) {
	if err := s.deviceFormLists(r, &f); err != nil {
		s.fail(w, r, err)
		return
	}
	var c templ.Component
	switch {
	case !isHTMX(r):
		u, _ := userFrom(r)
		c = views.DeviceFormPage(u.Username, f)
	case f.IsEdit:
		c = views.DeviceDrawer(f)
	default:
		c = views.DeviceDialog(f)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, c)
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
	name := strings.TrimSpace(r.FormValue("name"))
	kind := r.FormValue("kind")
	dev := store.Device{
		Name: name, Kind: kind, Notes: r.FormValue("notes"),
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
	if !validKinds[kind] {
		refuse(http.StatusBadRequest, "bad kind")
		return
	}
	if name == "" {
		refuse(http.StatusBadRequest, "name required")
		return
	}
	var macP *string
	if raw := strings.TrimSpace(r.FormValue("mac")); raw != "" {
		mac, ok := normMAC(raw)
		if !ok {
			refuse(http.StatusBadRequest, "invalid MAC address")
			return
		}
		macP = &mac
	}
	var subnetID int64
	ip := strings.TrimSpace(r.FormValue("ip"))
	if ip != "" {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			refuse(http.StatusBadRequest, "invalid IP address")
			return
		}
		sn, msg, err := s.subnetForForm(r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if msg != "" {
			refuse(http.StatusBadRequest, msg)
			return
		}
		prefix, err := netip.ParsePrefix(sn.CIDR)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !prefix.Contains(addr) {
			refuse(http.StatusBadRequest, "IP "+addr.String()+" is not in subnet "+sn.CIDR)
			return
		}
		subnetID, ip = sn.ID, addr.String()
	}
	devID, err := s.store.CreateDeviceWithIface(r.Context(), dev, macP, subnetID, ip, "static")
	if err != nil {
		if status, msg, ok := writeFailure(err, "that MAC address already belongs to another device", "parent device does not exist"); ok {
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
	redirectAfterForm(w, r, "/devices/"+strconv.FormatInt(devID, 10))
}

// subnetForForm loads the subnet named by the form's subnet_id. msg says what
// is wrong with the form when it is missing or names no subnet; err is a
// failure to look it up.
func (s *Server) subnetForForm(r *http.Request) (sn store.Subnet, msg string, err error) {
	id, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	if err != nil {
		return store.Subnet{}, "choose the subnet the IP belongs to", nil
	}
	sn, err = s.store.GetSubnet(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Subnet{}, "unknown subnet", nil
	}
	return sn, "", err
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
		if status, msg, ok := writeFailure(err, "device conflicts with an existing one", "parent device does not exist"); ok {
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
	http.Redirect(w, r, "/devices", http.StatusSeeOther)
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteDevice(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/devices", http.StatusSeeOther)
}

// handleDevicePage assembles the full device detail view: fields, per-iface
// IPs/ports/availability, tags, custom fields, links, parent/children and
// recent event history.
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
	since := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
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
	children, err := s.store.ListChildren(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var parent *store.Device
	if d.ParentDeviceID != nil {
		p, err := s.store.GetDevice(r.Context(), *d.ParentDeviceID)
		if err == nil {
			parent = &p
		}
	}
	evs, err := s.store.ListDeviceEvents(r.Context(), id, 20)
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
		AlertOffline: alert,
	}))
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
	if err := s.store.SetDeviceAlertOffline(r.Context(), id, r.FormValue("alert_offline") == "1"); err != nil {
		s.fail(w, r, err)
		return
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
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

// handleWOL sends a Wake-on-LAN magic packet to the MAC of the device's
// first interface that has one, then redirects back to the device page.
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
		if f.MAC != nil {
			if err := wol.Send(*f.MAC); err != nil {
				s.fail(w, r, err)
				return
			}
			http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
			return
		}
	}
	http.Error(w, "device has no MAC", 400)
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
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
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
