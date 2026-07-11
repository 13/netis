package web

import (
	"fmt"
	"net/http"
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
	ips, err := scan.HostIPs(sn.CIDR)
	if err != nil {
		return nil, err
	}
	cells := make([]views.GridCell, 0, len(ips))
	for _, ip := range ips {
		c := views.GridCell{IP: ip, State: "free", Title: ip}
		if o, ok := occ[ip]; ok {
			c.DeviceID = o.DeviceID
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
	u, _ := userFrom(r)
	views.GridPage(u.Username, sn, cells).Render(r.Context(), w)
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
	if s.trigger != nil {
		s.trigger.Trigger(sn.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
