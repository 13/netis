package web

import (
	"net/http"
	"strings"

	"netis/internal/scan"
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

// searchFree is a subnet's lowest address free for static use, for the
// palette's "free" query: the address, the subnet, and how many are free.
type searchFree struct {
	SubnetID int64  `json:"subnet_id"`
	Name     string `json:"name"`
	CIDR     string `json:"cidr"`
	IP       string `json:"ip"`
	Free     int    `json:"free"`
}

type searchResult struct {
	Devices []searchDevice `json:"devices"`
	Subnets []searchSubnet `json:"subnets"`
	Free    []searchFree   `json:"free,omitempty"`
}

// freeQuery reports whether q asks for free addresses ("free", "free lab"),
// and the rest of it, which narrows the subnets.
func freeQuery(q string) (string, bool) {
	f := strings.Fields(strings.ToLower(q))
	if len(f) == 0 || f[0] != "free" {
		return "", false
	}
	return strings.Join(f[1:], " "), true
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
	if rest, ok := freeQuery(q); ok {
		free, err := s.searchFree(r, rest)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		res.Free = free
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

// searchFree lists, for each subnet whose name or CIDR contains q, its
// lowest address free for static use. Subnets with none left are skipped.
func (s *Server) searchFree(r *http.Request, q string) ([]searchFree, error) {
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		return nil, err
	}
	out := []searchFree{}
	for _, sn := range subnets {
		if len(out) == searchLimit {
			break
		}
		if q != "" && !strings.Contains(strings.ToLower(sn.Name+" "+sn.CIDR), q) {
			continue
		}
		hosts, err := scan.HostIPs(sn.CIDR)
		if err != nil {
			continue
		}
		occ, err := s.store.SubnetOccupancy(r.Context(), sn.ID)
		if err != nil {
			return nil, err
		}
		if next, n, _ := freeSummary(sn, hosts, occ); next != "" {
			out = append(out, searchFree{SubnetID: sn.ID, Name: sn.Name, CIDR: sn.CIDR, IP: next, Free: n})
		}
	}
	return out, nil
}
