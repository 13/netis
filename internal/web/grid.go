package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
)

func (s *Server) gridCells(ctx context.Context, sn store.Subnet) ([]views.GridCell, error) {
	occ, err := s.store.SubnetOccupancy(ctx, sn.ID)
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
			c.Title = strings.TrimSpace(fmt.Sprintf("%s — %s %s", ip, o.DeviceName, o.MAC))
			if o.LastSeen != "" {
				c.Title += " last seen " + views.RelTime(o.LastSeen)
			}
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
	return s.store.GetSubnet(r.Context(), id)
}

// devicesInSubnet returns the DeviceRows that have an IP assigned in subnetID,
// in ListDevices order.
func (s *Server) devicesInSubnet(ctx context.Context, subnetID int64) ([]store.DeviceRow, error) {
	all, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListSubnetIfaceIPs(ctx, subnetID)
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

// ScanStatusReporter reports where scanning of a subnet stands. The
// scheduler implements it; a ScanTrigger without it (tests) leaves the scan
// control showing only the schedule.
type ScanStatusReporter interface {
	Status(subnetID int64) scan.Status
}

func (s *Server) scanState(sn store.Subnet) views.ScanState {
	st := views.ScanState{Subnet: sn}
	rep, ok := s.trigger.(ScanStatusReporter)
	if !ok {
		return st
	}
	x := rep.Status(sn.ID)
	st.Queued, st.Running, st.LastOK = x.Queued, x.Running, x.LastOK
	if !x.LastDone.IsZero() {
		st.LastDone = x.LastDone.UTC().Format(time.RFC3339)
	}
	return st
}

// gridStats counts the cells by state and the held addresses by lease kind.
func gridStats(cells []views.GridCell) views.GridStats {
	var st views.GridStats
	for _, c := range cells {
		switch c.State {
		case "online":
			st.Online++
		case "offline":
			st.Offline++
		case "reserved":
			st.Unseen++
		case "conflict":
			st.Conflict++
		case "free":
			st.Free++
		}
		switch c.Kind {
		case "static":
			st.Static++
		case "dhcp":
			st.DHCP++
		}
	}
	return st
}

// nextFree is the first free address in the grid, "" when none is left.
func nextFree(cells []views.GridCell) string {
	for _, c := range cells {
		if c.State == "free" {
			return c.IP
		}
	}
	return ""
}

// cellInfo gathers the details panel's content for ip, reporting false when
// ip is not an address of the subnet.
func (s *Server) cellInfo(ctx context.Context, sn store.Subnet, cells []views.GridCell, ip string) (*views.CellInfo, bool, error) {
	for i, c := range cells {
		if c.IP != ip {
			continue
		}
		info := &views.CellInfo{IP: ip, State: c.State}
		if c.State == "edge" {
			info.Edge = "network address"
			if i > 0 {
				info.Edge = "broadcast address"
			}
			return info, true, nil
		}
		if c.DeviceID > 0 {
			claims, err := s.store.IPClaims(ctx, sn.ID, ip)
			if err != nil {
				return nil, true, err
			}
			info.Claims = claims
		}
		return info, true, nil
	}
	return nil, false, nil
}

// gridPageData assembles the subnet page. selected, when it is an address of
// the subnet, opens that port's details. A subnet too large to draw answers
// with the size error for the caller to show.
func (s *Server) gridPageData(r *http.Request, sn store.Subnet, selected string) (views.GridPageData, error) {
	cells, err := s.gridCells(r.Context(), sn)
	if err != nil {
		return views.GridPageData{}, err
	}
	u, _ := userFrom(r)
	d := views.GridPageData{
		Username: u.Username, Subnet: sn, Cells: cells,
		Stats: gridStats(cells), NextFree: nextFree(cells), Scan: s.scanState(sn),
	}
	if selected != "" {
		info, ok, err := s.cellInfo(r.Context(), sn, cells, selected)
		if err != nil {
			return d, err
		}
		if ok {
			d.Selected, d.Cell = selected, info
		}
	}
	return d, nil
}

// gridError answers a failure to lay out a subnet's grid. A subnet saved
// before the size limit existed, or edited around it, is a configuration
// problem and not a server fault; its message is written to be shown to a
// user.
func (s *Server) gridError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, scan.ErrSubnetTooLarge) {
		http.Error(w, err.Error(), 400)
		return
	}
	s.fail(w, r, err)
}

func (s *Server) handleSubnetPage(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderGridPage(w, r, sn, r.URL.Query().Get("ip"))
}

func (s *Server) renderGridPage(w http.ResponseWriter, r *http.Request, sn store.Subnet, selected string) {
	d, err := s.gridPageData(r, sn, selected)
	if err != nil {
		s.gridError(w, r, err)
		return
	}
	d.Devices, err = s.devicesInSubnet(r.Context(), sn.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d.SortKey, d.Dir = parseDeviceSort(r)
	sortDeviceRows(d.Devices, d.SortKey, d.Dir)
	s.render(w, r, views.GridPage(d))
}

// handleGridFrag answers the live refresh of a subnet's panel: the ports,
// with the counts, next free address and scan control out of band.
func (s *Server) handleGridFrag(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.gridPageData(r, sn, "")
	if err != nil {
		s.gridError(w, r, err)
		return
	}
	s.render(w, r, views.GridRefresh(d))
}

// onSubnetPages reports whether an htmx request came from a page that shows
// a subnet's scan control (the subnets index or a subnet page), so the scan
// response only swaps one in where there is one to replace.
func onSubnetPages(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("HX-Current-URL"))
	return err == nil && (u.Path == "/subnets" || strings.HasPrefix(u.Path, "/subnets/"))
}

func (s *Server) handleScanNow(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sn.Kind == "wireguard" {
		s.render(w, r, views.ScanToast(sn.CIDR+" is WireGuard — not scannable"))
		return
	}
	if s.trigger != nil && !s.trigger.Trigger(sn.ID) {
		s.render(w, r, views.ScanToast("Scan of "+sn.CIDR+" already queued or running"))
	} else {
		s.render(w, r, views.ScanToast("Scanning "+sn.CIDR+"…"))
	}
	if onSubnetPages(r) {
		s.render(w, r, views.ScanControlOOB(s.scanState(sn)))
	}
}

// handleCellDetail answers a click on a port. htmx gets the details panel
// alone; a plain request (no JavaScript, or a shared link) gets the whole
// subnet page with that port open.
func (s *Server) handleCellDetail(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ip := r.URL.Query().Get("ip")
	if !isHTMX(r) {
		s.renderGridPage(w, r, sn, ip)
		return
	}
	cells, err := s.gridCells(r.Context(), sn)
	if err != nil {
		s.gridError(w, r, err)
		return
	}
	info, ok, err := s.cellInfo(r.Context(), sn, cells, ip)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, views.CellPanel(sn, *info))
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
	if err := s.store.SetIPKind(r.Context(), sn.ID, ip, kind); err != nil {
		s.fail(w, r, err)
		return
	}
	s.broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	label := "static"
	if kind == "dhcp" {
		label = "DHCP"
	}
	s.render(w, r, views.ScanToast(ip+" is now "+label))
}

// handleSubnetsIndex lists the subnets, each with a miniature of its panel
// (or a usage bar when it is larger than a /24), its counts and its scan
// state.
func (s *Server) handleSubnetsIndex(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]views.SubnetSummary, 0, len(subnets))
	for _, sn := range subnets {
		sum := views.SubnetSummary{Subnet: sn, Scan: s.scanState(sn)}
		cells, err := s.gridCells(r.Context(), sn)
		switch {
		case errors.Is(err, scan.ErrSubnetTooLarge):
			// Listed without counts; its own page explains the limit.
		case err != nil:
			s.fail(w, r, err)
			return
		default:
			sum.Stats = gridStats(cells)
			if hosts, err := scan.HostIPs(sn.CIDR); err == nil {
				sum.Hosts = len(hosts)
			}
			if len(cells) <= views.MiniPanelMax {
				sum.Cells = cells
			}
		}
		out = append(out, sum)
	}
	u, _ := userFrom(r)
	s.render(w, r, views.SubnetsPage(u.Username, out))
}

func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	queued, skipped := 0, 0
	if s.trigger != nil {
		for _, sn := range subnets {
			if sn.Kind == "wireguard" {
				continue
			}
			if s.trigger.Trigger(sn.ID) {
				queued++
			} else {
				skipped++
			}
		}
	}
	switch {
	case skipped == 0:
		s.render(w, r, views.ScanToast("Scanning all subnets…"))
	case queued == 0:
		s.render(w, r, views.ScanToast("Scans already queued or running"))
	default:
		s.render(w, r, views.ScanToast(fmt.Sprintf("Scanning %d subnet(s); %d already queued or running", queued, skipped)))
	}
}
