package web

import (
	"net/http"
	"sort"

	"netis/internal/scan"
	"netis/internal/web/views"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var rows []views.DashRow
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(sn.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		hosts, _ := scan.HostIPs(sn.CIDR)
		free := len(hosts) - len(occ)
		if free < 0 {
			free = 0
		}
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: free}
		seen := make(map[string]bool)
		for _, o := range occ {
			if o.Online {
				row.Online++
			}
			if !seen[o.DeviceName] {
				seen[o.DeviceName] = true
				row.Occupants = append(row.Occupants, o.DeviceName)
			}
		}
		sort.Strings(row.Occupants)
		rows = append(rows, row)
	}
	evs, _ := s.store.ListEvents(15)
	views.Dashboard(u.Username, rows, evs).Render(r.Context(), w)
}
