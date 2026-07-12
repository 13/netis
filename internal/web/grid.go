package web

import (
	"fmt"
	"net/http"
	"net/netip"
	"strconv"

	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
)

func (s *Server) gridCells(sn store.Subnet) ([]views.GridCell, error) {
	occ, err := s.store.SubnetOccupancy(sn.ID)
	if err != nil {
		return nil, err
	}
	ips, err := scan.AllIPs(sn.CIDR)
	if err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(sn.CIDR)
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	hasEdges := prefix.Addr().Is4() && prefix.Bits() < 31
	cells := make([]views.GridCell, 0, len(ips))
	for i, ip := range ips {
		c := views.GridCell{IP: ip, State: "free", Title: ip}
		if hasEdges && (i == 0 || i == len(ips)-1) {
			c.State = "edge"
			if i == 0 {
				c.Title = ip + " — network address"
			} else {
				c.Title = ip + " — broadcast address"
			}
			cells = append(cells, c)
			continue
		}
		if o, ok := occ[ip]; ok {
			c.DeviceID = o.DeviceID
			c.Kind = o.Kind
			c.Title = fmt.Sprintf("%s — %s %s last seen %s", ip, o.DeviceName, o.MAC, o.LastSeen)
			switch {
			case o.Count > 1:
				c.State = "conflict"
			case !o.EverSeen:
				c.State = "reserved"
			case o.Online:
				c.State = "online"
			default:
				c.State = "offline"
			}
		}
		cells = append(cells, c)
	}
	return cells, nil
}

func (s *Server) subnetFromPath(r *http.Request) (store.Subnet, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return store.Subnet{}, err
	}
	return s.store.GetSubnet(id)
}

// devicesInSubnet returns the DeviceRows that have an IP assigned in subnetID,
// in ListDevices order.
func (s *Server) devicesInSubnet(subnetID int64) ([]store.DeviceRow, error) {
	all, err := s.store.ListDevices()
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListSubnetIfaceIPs(subnetID)
	if err != nil {
		return nil, err
	}
	inSubnet := make(map[int64]bool, len(links))
	for _, l := range links {
		inSubnet[l.DeviceID] = true
	}
	var out []store.DeviceRow
	for _, d := range all {
		if inSubnet[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *Server) handleSubnetPage(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cells, err := s.gridCells(sn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	devices, err := s.devicesInSubnet(sn.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	sortKey, dir := parseDeviceSort(r)
	sortDeviceRows(devices, sortKey, dir)
	u, _ := userFrom(r)
	views.GridPage(u.Username, sn, cells, devices, sortKey, dir).Render(r.Context(), w)
}

func (s *Server) handleGridFrag(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cells, err := s.gridCells(sn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.GridFrag(sn, cells).Render(r.Context(), w)
}

func (s *Server) handleScanNow(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sn.Kind == "wireguard" {
		views.ScanToast(sn.CIDR+" is WireGuard — not scannable").Render(r.Context(), w)
		return
	}
	if s.trigger != nil {
		s.trigger.Trigger(sn.ID)
	}
	views.ScanToast("Scanning "+sn.CIDR+"…").Render(r.Context(), w)
}

func (s *Server) handleCellDetail(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ip := r.URL.Query().Get("ip")
	occ, err := s.store.SubnetOccupancy(sn.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.CellDetail(sn, ip, occ[ip]).Render(r.Context(), w)
}

func (s *Server) handleCellKind(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ip := r.FormValue("ip")
	kind := r.FormValue("kind")
	if kind != "static" && kind != "dhcp" {
		http.Error(w, "bad kind", 400)
		return
	}
	if err := s.store.SetIPKind(sn.ID, ip, kind); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	views.ScanToast(ip+" → "+kind).Render(r.Context(), w)
}

func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if s.trigger != nil {
		for _, sn := range subnets {
			if sn.Kind != "wireguard" {
				s.trigger.Trigger(sn.ID)
			}
		}
	}
	views.ScanToast("Scanning all subnets…").Render(r.Context(), w)
}
