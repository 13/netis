package web

import (
	"net/http"
	"sort"
	"strings"

	"netis/internal/scan"
	"netis/internal/web/views"
)

// subnetRows builds the per-subnet occupancy rows (shared by the dashboard and
// the subnets index) plus any IP conflicts found while scanning occupancy.
func (s *Server) subnetRows() ([]views.DashRow, []views.AttentionConflict, error) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return nil, nil, err
	}
	var rows []views.DashRow
	var conflicts []views.AttentionConflict
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(sn.ID)
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
				row.Reserved++
			default:
				row.Offline++
			}
			if o.Count > 1 {
				conflicts = append(conflicts, views.AttentionConflict{IP: ip, SubnetID: sn.ID, SubnetName: sn.Name})
			}
		}
		rows = append(rows, row)
	}
	return rows, conflicts, nil
}

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
		if d.Source == "scan" && strings.HasPrefix(d.Name, "unknown-") && !d.Reviewed {
			data.Stats.Unknown++
			data.Unknowns = append(data.Unknowns, views.AttentionUnknown{ID: d.ID, Name: d.Name})
		}
	}
	data.Stats.Offline = data.Stats.Total - data.Stats.Online

	rows, conflicts, err := s.subnetRows()
	if err != nil {
		return data, err
	}
	data.Rows = rows
	data.Stats.Subnets = len(rows)
	data.Conflicts = conflicts
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

func (s *Server) handleSubnetsIndex(w http.ResponseWriter, r *http.Request) {
	rows, _, err := s.subnetRows()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u, _ := userFrom(r)
	views.SubnetsPage(u.Username, rows).Render(r.Context(), w)
}
