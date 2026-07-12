package web

import (
	"net/http"
	"sort"
	"strings"

	"netis/internal/scan"
	"netis/internal/web/views"
)

func (s *Server) assembleDashboard(r *http.Request) (views.DashboardData, error) {
	u, _ := userFrom(r)
	data := views.DashboardData{Username: u.Username}

	devices, err := s.store.ListDevices()
	if err != nil {
		return data, err
	}
	data.Stats.Total = len(devices)
	for _, d := range devices {
		if d.Online {
			data.Stats.Online++
		}
		if d.Source == "scan" && strings.HasPrefix(d.Name, "unknown-") {
			data.Stats.Unknown++
			data.Unknowns = append(data.Unknowns, views.AttentionUnknown{ID: d.ID, Name: d.Name})
		}
	}
	data.Stats.Offline = data.Stats.Total - data.Stats.Online

	subnets, err := s.store.ListSubnets()
	if err != nil {
		return data, err
	}
	data.Stats.Subnets = len(subnets)
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(sn.ID)
		if err != nil {
			return data, err
		}
		hosts, _ := scan.HostIPs(sn.CIDR)
		free := len(hosts) - len(occ)
		if free < 0 {
			free = 0
		}
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: free}
		seen := make(map[string]bool)
		for ip, o := range occ {
			if o.Online {
				row.Online++
			}
			if o.Count > 1 {
				data.Conflicts = append(data.Conflicts, views.AttentionConflict{
					IP: ip, SubnetID: sn.ID, SubnetName: sn.Name,
				})
			}
			if !seen[o.DeviceName] {
				seen[o.DeviceName] = true
				row.Occupants = append(row.Occupants, o.DeviceName)
			}
		}
		sort.Strings(row.Occupants)
		data.Rows = append(data.Rows, row)
	}
	sort.Slice(data.Conflicts, func(i, j int) bool { return data.Conflicts[i].IP < data.Conflicts[j].IP })

	statuses, err := s.store.ListIntegrationStatus()
	if err != nil {
		return data, err
	}
	data.Integrations = statuses

	evs, err := s.store.ListEvents(15)
	if err != nil {
		return data, err
	}
	data.Events = evs
	return data, nil
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.assembleDashboard(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.Dashboard(data).Render(r.Context(), w)
}

func (s *Server) handleDashboardWidgets(w http.ResponseWriter, r *http.Request) {
	data, err := s.assembleDashboard(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.DashboardBody(data).Render(r.Context(), w)
}
