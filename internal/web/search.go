package web

import (
	"net/http"
	"strings"

	"netis/internal/web/views"
)

// searchLimit caps each kind of result the command palette gets back: it
// shows a handful, and the full list is one Enter away on the Devices page.
const searchLimit = 8

// searchDevice is a device as the command palette lists it.
type searchDevice struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip,omitempty"`
	MAC    string `json:"mac,omitempty"`
	Icon   string `json:"icon"`
	Online bool   `json:"online"`
}

// searchSubnet is a subnet as the command palette lists it.
type searchSubnet struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

type searchResult struct {
	Devices []searchDevice `json:"devices"`
	Subnets []searchSubnet `json:"subnets"`
}

// handleSearch answers the command palette: devices matching q by name, IP,
// MAC, tag, vendor, model or function (the device list's own filter), and
// subnets by name or CIDR. It only reads what every role can already see on
// the Devices and Subnets pages.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res := searchResult{Devices: []searchDevice{}, Subnets: []searchSubnet{}}
	if q == "" {
		s.writeJSON(w, r, res)
		return
	}
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows = filterDevices(rows, q)
	sortDeviceRows(rows, "name", "asc")
	for _, row := range rows {
		if len(res.Devices) == searchLimit {
			break
		}
		d := searchDevice{ID: row.ID, Name: row.Name, Online: row.Online, Icon: views.DeviceIcon(row.Icon, row.Kind)}
		if ip, ok := lowestIP(row); ok {
			d.IP = ip.String()
		}
		if len(row.MACs) > 0 {
			d.MAC = row.MACs[0]
		}
		res.Devices = append(res.Devices, d)
	}
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lq := strings.ToLower(q)
	for _, sn := range subnets {
		if len(res.Subnets) == searchLimit {
			break
		}
		if strings.Contains(strings.ToLower(sn.Name+" "+sn.CIDR), lq) {
			res.Subnets = append(res.Subnets, searchSubnet{ID: sn.ID, Name: sn.Name, CIDR: sn.CIDR})
		}
	}
	s.writeJSON(w, r, res)
}
