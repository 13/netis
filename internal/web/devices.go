package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
	"netis/internal/wol"
)

var validKinds = map[string]bool{"computer": true, "switch": true, "phone": true,
	"server": true, "printer": true, "iot": true, "vm": true, "lxc": true,
	"wg-peer": true, "router": true, "modem": true, "other": true}

func normMAC(in string) string {
	m := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(in, "-", ":")))
	if len(m) != 17 {
		return ""
	}
	return m
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
	rows, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
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

	u, _ := userFrom(r)
	views.DeviceList(u.Username, rows, r.URL.Query().Get("q"), sortKey, dir).Render(r.Context(), w)
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

func (s *Server) handleDeviceForm(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	all, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var subnetID int64
	if v := r.URL.Query().Get("subnet"); v != "" {
		subnetID, _ = strconv.ParseInt(v, 10, 64)
	}
	views.DeviceDialog(store.Device{Kind: "computer"}, nil, subnets, all, false, subnetID).Render(r.Context(), w)
}

func (s *Server) handleDeviceEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	tags, err := s.store.DeviceTags(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	all, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.DeviceDrawer(d, tags, subnets, all, 0).Render(r.Context(), w)
}

func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	kind := r.FormValue("kind")
	if !validKinds[kind] {
		http.Error(w, "bad kind", 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
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
	devID, err := s.store.CreateDevice(dev)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetDeviceTags(devID, parseTags(r.FormValue("tags"))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if mac := normMAC(r.FormValue("mac")); mac != "" || r.FormValue("ip") != "" {
		var macP *string
		if mac != "" {
			macP = &mac
		}
		ifID, err := s.store.AddIface(devID, macP, nil)
		if err == nil && r.FormValue("ip") != "" {
			if snID, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64); err == nil {
				s.store.AssignIP(ifID, snID, r.FormValue("ip"), "static")
			}
		}
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(devID, 10), http.StatusSeeOther)
}

func (s *Server) handleDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
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
	if err := s.store.UpdateDevice(d); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetDeviceTags(d.ID, parseTags(r.FormValue("tags"))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetDeviceReviewed(id, true); err != nil {
		http.Error(w, err.Error(), 500)
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
	if err := s.store.DeleteDevice(id); err != nil {
		http.Error(w, err.Error(), 500)
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
	d, err := s.store.GetDevice(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}

	ifaces, err := s.store.ListIfaces(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	ifaceDetails := make([]views.IfaceDetail, 0, len(ifaces))
	for _, f := range ifaces {
		ips, err := s.store.ListIPs(f.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		ports, err := s.store.ListOpenPorts(f.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		pct, err := s.store.AvailabilityPct(f.ID, since)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		online, lastSeen, err := s.store.IfaceOnline(f.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
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

	tags, err := s.store.DeviceTags(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fields, err := s.store.ListCustomFields(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	links, err := s.store.ListLinks(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	children, err := s.store.ListChildren(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var parent *store.Device
	if d.ParentDeviceID != nil {
		p, err := s.store.GetDevice(*d.ParentDeviceID)
		if err == nil {
			parent = &p
		}
	}
	evs, err := s.store.ListDeviceEvents(id, 20)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	u, _ := userFrom(r)
	views.DevicePage(u.Username, views.DeviceDetail{
		Device: d, Ifaces: ifaceDetails, Tags: tags,
		Fields: fields, Links: links, Children: children, Parent: parent, Events: evs,
	}).Render(r.Context(), w)
}

func (s *Server) handleLinkAdd(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	label := strings.TrimSpace(r.FormValue("label"))
	url := strings.TrimSpace(r.FormValue("url"))
	if label != "" && url != "" {
		if _, err := s.store.AddLink(id, label, url); err != nil {
			http.Error(w, err.Error(), 500)
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
	if err := s.store.DeleteLink(linkID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices/"+devID, http.StatusSeeOther)
}

func (s *Server) handleFieldSet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	key := strings.TrimSpace(r.FormValue("key"))
	if key != "" {
		if err := s.store.SetCustomField(id, key, r.FormValue("value")); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleFieldDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	key := r.FormValue("key")
	if err := s.store.DeleteCustomField(id, key); err != nil {
		http.Error(w, err.Error(), 500)
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
	if _, err := s.store.GetDevice(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	ifaces, err := s.store.ListIfaces(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, f := range ifaces {
		if f.MAC != nil {
			if err := wol.Send(*f.MAC); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
			return
		}
	}
	http.Error(w, "device has no MAC", 400)
}

// handlePortScan runs an on-demand TCP port scan against the first IP of
// the device's first interface, upserts any open ports found, and
// redirects back to the device page.
func (s *Server) handlePortScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetDevice(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	ifaces, err := s.store.ListIfaces(id)
	if err != nil || len(ifaces) == 0 {
		http.Error(w, "device has no interface", 400)
		return
	}
	ips, err := s.store.ListIPs(ifaces[0].ID)
	if err != nil || len(ips) == 0 {
		http.Error(w, "device has no IP", 400)
		return
	}
	open := scan.PortScan(r.Context(), ips[0].IP, scan.CommonPorts, time.Second)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, p := range open {
		s.store.UpsertOpenPort(ifaces[0].ID, p, "tcp", scan.ServiceGuess(p), now)
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
	if err := s.store.SetIPKind(subnetID, ip, kind); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.LeaseToggle(devID, subnetID, ip, kind).Render(r.Context(), w)
}
