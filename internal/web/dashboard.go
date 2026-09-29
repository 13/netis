package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"netis/internal/clock"
	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
)

// subnetRows builds the per-subnet occupancy rows (shared by the dashboard and
// the subnets index) plus any IP conflicts found while scanning occupancy.
func (s *Server) subnetRows(ctx context.Context) ([]views.DashRow, []views.AttentionConflict, error) {
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return nil, nil, err
	}
	var rows []views.DashRow
	var conflicts []views.AttentionConflict
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(ctx, sn.ID)
		if err != nil {
			return nil, nil, err
		}
		hosts, _ := scan.HostIPs(sn.CIDR)
		free := len(hosts) - len(occ)
		if free < 0 {
			free = 0
		}
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: free, Hosts: len(hosts)}
		for ip, o := range occ {
			switch {
			case o.Online:
				row.Online++
			case !o.EverSeen:
				row.Unseen++
			default:
				row.Offline++
			}
			// A reservation is stored as a static assignment, and it stays
			// one whether or not its device is up.
			if o.Kind == "static" {
				row.Reserved++
			}
			if o.Count > 1 {
				conflicts = append(conflicts, views.AttentionConflict{IP: ip, SubnetID: sn.ID, SubnetName: sn.Name})
			}
		}
		rows = append(rows, row)
	}
	return rows, conflicts, nil
}

// dashNewLimit caps how many unreviewed devices the attention list names;
// the rest are counted and linked to the device list.
const dashNewLimit = 5

// dashEventLimit is how many recent events the dashboard shows.
const dashEventLimit = 12

func (s *Server) assembleDashboard(r *http.Request) (views.DashboardData, error) {
	ctx := r.Context()
	u, _ := userFrom(r)
	data := views.DashboardData{Username: u.Username, ConflictHref: "#attention"}

	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return data, err
	}
	alerting, err := s.store.AlertOfflineDeviceIDs(ctx)
	if err != nil {
		return data, err
	}
	var conflicts, failing, offline, fresh, upstream []views.AttentionItem
	for _, d := range devices {
		if d.Online {
			data.Health.Online++
		} else {
			data.Health.Offline++
		}
		href := fmt.Sprintf("/devices/%d", d.ID)
		if !d.Reviewed {
			data.Health.New++
			if len(fresh) < dashNewLimit {
				it := views.AttentionItem{Kind: views.AttentionNewKind, Title: d.Name, Href: href, DeviceID: d.ID, Detail: d.Vendor}
				// A discovery is often named after its address; say it once.
				if len(d.IPs) > 0 && d.IPs[0].IP != d.Name {
					it.IP = d.IPs[0].IP
				}
				fresh = append(fresh, it)
			} else {
				data.MoreNew++
			}
		}
		if !d.Online && alerting[d.ID] {
			it := views.AttentionItem{Kind: views.AttentionOfflineKind, Title: d.Name, Href: href, Detail: "Last seen"}
			if d.LastSeen != nil {
				it.Since = *d.LastSeen
			} else {
				it.Detail = "Never seen"
			}
			offline = append(offline, it)
		}
		if d.UpstreamMissingSince != nil {
			where := "upstream"
			if t, ok := views.IntegrationTitles[d.Source]; ok {
				where = "in " + t
			}
			upstream = append(upstream, views.AttentionItem{
				Kind: views.AttentionUpstreamKind, Title: d.Name, Href: href,
				Detail: "No longer listed " + where + " since", Since: *d.UpstreamMissingSince,
			})
		}
	}

	rows, found, err := s.subnetRows(ctx)
	if err != nil {
		return data, err
	}
	data.Rows = rows
	subnets := make([]store.Subnet, len(rows))
	for i, row := range rows {
		subnets[i] = row.Subnet
	}
	if data.Setup, err = s.setupStatus(r, subnets); err != nil {
		return data, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].IP < found[j].IP })
	for _, c := range found {
		href := fmt.Sprintf("/subnets/%d?ip=%s", c.SubnetID, url.QueryEscape(c.IP))
		conflicts = append(conflicts, views.AttentionItem{
			Kind: views.AttentionConflictKind, Title: c.IP + " is claimed by more than one device",
			Detail: "In " + c.SubnetName, Href: href,
		})
	}
	data.Health.Conflicts = len(conflicts)
	if len(conflicts) == 1 {
		data.ConflictHref = conflicts[0].Href
	}

	statuses, err := s.store.ListIntegrationStatus(ctx)
	if err != nil {
		return data, err
	}
	data.Integrations = statuses
	data.Unscanned = unscanned(rows, statuses)
	for _, st := range statuses {
		if st.OK {
			continue
		}
		data.Health.Failing++
		it := views.AttentionItem{
			Kind: views.AttentionIntegrationKind, Title: views.IntegrationName(st.Name) + " is failing",
			Detail: failureDetail(st.Detail), Since: st.LastRun,
		}
		if _, ok := views.IntegrationTitles[st.Name]; ok {
			it.Integration = st.Name
			if isAdmin(r) {
				it.Href = "/settings/integrations"
			}
		} else if isAdmin(r) {
			it.Href = "/settings/network"
		}
		failing = append(failing, it)
	}

	for _, group := range [][]views.AttentionItem{conflicts, failing, offline, fresh, upstream} {
		data.Attention = append(data.Attention, group...)
	}

	evs, err := s.store.ListEvents(ctx, dashEventLimit)
	if err != nil {
		return data, err
	}
	data.Days = views.GroupEventsByDay(evs, clock.Now())
	return data, nil
}

// unscanned reports whether nothing has been scanned yet: there is no subnet,
// or there are subnets a sweep covers and the scanner has never recorded a
// run. A WireGuard subnet is read from its server, never swept.
func unscanned(rows []views.DashRow, statuses []store.IntegrationStatus) bool {
	if len(rows) == 0 {
		return true
	}
	for _, st := range statuses {
		if st.Name == "scan" {
			return false
		}
	}
	for _, row := range rows {
		if row.Subnet.Kind != "wireguard" {
			return true
		}
	}
	return false
}

// failureDetail puts a stored failure detail ("timeout", "auth failed: 401
// Unauthorized") in sentence case for the attention list.
func failureDetail(d string) string {
	if d == "" {
		return "Last run failed"
	}
	return strings.ToUpper(d[:1]) + d[1:]
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.assembleDashboard(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.Dashboard(data))
}

func (s *Server) handleDashboardWidgets(w http.ResponseWriter, r *http.Request) {
	data, err := s.assembleDashboard(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, views.DashboardBody(data))
}
