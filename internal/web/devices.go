package web

import (
	"database/sql"
	"errors"
	"net/http"
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
	"wg-peer": true, "other": true}

func normMAC(in string) string {
	m := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(in, "-", ":")))
	if len(m) != 17 {
		return ""
	}
	return m
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
			hay := strings.ToLower(row.Name + " " + strings.Join(row.IPs, " ") + " " +
				strings.Join(row.MACs, " ") + " " + strings.Join(row.TagNames, " "))
			if strings.Contains(hay, q) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	u, _ := userFrom(r)
	views.DeviceList(u.Username, rows, r.URL.Query().Get("q")).Render(r.Context(), w)
}

func (s *Server) handleDeviceForm(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u, _ := userFrom(r)
	views.DeviceForm(u.Username, subnets).Render(r.Context(), w)
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
	devID, err := s.store.CreateDevice(store.Device{
		Name: name, Kind: kind, Notes: r.FormValue("notes"), Source: "manual",
	})
	if err != nil {
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
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
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
	allTags, err := s.store.ListTags()
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
		Device: d, Ifaces: ifaceDetails, Tags: tags, AllTags: allTags,
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

func (s *Server) handleTagAdd(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	if name != "" {
		tagID, err := s.findOrCreateTag(name)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := s.store.TagDevice(id, tagID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) findOrCreateTag(name string) (int64, error) {
	tags, err := s.store.ListTags()
	if err != nil {
		return 0, err
	}
	for _, t := range tags {
		if t.Name == name {
			return t.ID, nil
		}
	}
	return s.store.CreateTag(name, "#888888")
}

func (s *Server) handleTagRemove(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	tagID, _ := strconv.ParseInt(r.PathValue("tagID"), 10, 64)
	if err := s.store.UntagDevice(id, tagID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
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
