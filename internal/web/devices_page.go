package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"netis/internal/autofill"
	"netis/internal/clock"
	"netis/internal/store"
	"netis/internal/web/views"
)

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
	now := clock.Now()
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
	allTags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tagColors := make(views.TagColors, len(allTags))
	for _, t := range allTags {
		tagColors[t.Name] = t.Color
	}
	hints, err := s.store.ListHints(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	recs, err := s.store.ListAutofill(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tagNames := make([]string, len(tags))
	for i, t := range tags {
		tagNames[i] = t.Name
	}
	autofilled := map[string]store.AutofillRecord{}
	for _, rec := range recs {
		if rec.State == store.AutofillApplied {
			autofilled[rec.Field] = rec
		}
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
		Detected:     autofill.Explain(d, tagNames, recs, hints),
		Autofilled:   autofilled,
		TagColors:    tagColors,
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
