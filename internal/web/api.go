package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"netis/internal/buildinfo"
	"netis/internal/store"
)

// The JSON API is read-only on purpose. Everything that changes state is a
// form POST from the UI, guarded by role checks and cross-origin protection;
// exposing writes here would mean a second surface to keep in step with those.
// Reading is what a script actually wants: a dashboard, a backup of the
// inventory, a check that a host came back.
//
// Authentication is the same session cookie the pages use, so `curl -b` with a
// browser cookie works and there is no second credential to leak.

// apiDevice is the device shape the API returns. It is written out by hand
// rather than marshalling store.DeviceRow so a schema change cannot silently
// rename a field callers depend on.
type apiDevice struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Source   string   `json:"source"`
	Vendor   string   `json:"vendor,omitempty"`
	Model    string   `json:"model,omitempty"`
	Function string   `json:"function,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	Icon     string   `json:"icon,omitempty"`
	Reviewed bool     `json:"reviewed"`
	Online   bool     `json:"online"`
	LastSeen *string  `json:"last_seen"`
	IPs      []apiIP  `json:"ips"`
	MACs     []string `json:"macs"`
	Tags     []string `json:"tags"`
	ParentID *int64   `json:"parent_device_id,omitempty"`
	VMID     *int64   `json:"proxmox_vmid,omitempty"`
}

type apiIP struct {
	IP   string `json:"ip"`
	Kind string `json:"kind"`
}

func toAPIDevice(row store.DeviceRow) apiDevice {
	ips := make([]apiIP, 0, len(row.IPs))
	for _, ip := range row.IPs {
		ips = append(ips, apiIP{IP: ip.IP, Kind: ip.Kind})
	}
	// Empty slices rather than nil: a caller iterating the field should not
	// have to special-case null.
	macs := row.MACs
	if macs == nil {
		macs = []string{}
	}
	tags := row.TagNames
	if tags == nil {
		tags = []string{}
	}
	return apiDevice{
		ID: row.ID, Name: row.Name, Kind: row.Kind, Source: row.Source,
		Vendor: row.Vendor, Model: row.Model, Function: row.Function,
		Notes: row.Notes, Icon: row.Icon, Reviewed: row.Reviewed,
		Online: row.Online, LastSeen: row.LastSeen,
		IPs: ips, MACs: macs, Tags: tags,
		ParentID: row.ParentDeviceID, VMID: row.ProxmoxVMID,
	}
}

type apiSubnet struct {
	ID              int64  `json:"id"`
	CIDR            string `json:"cidr"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	VLANID          *int64 `json:"vlan_id,omitempty"`
	ScanEnabled     bool   `json:"scan_enabled"`
	ScanIntervalSec int    `json:"scan_interval_sec"`
}

type apiEvent struct {
	ID       int64  `json:"id"`
	TS       string `json:"ts"`
	Type     string `json:"type"`
	DeviceID *int64 `json:"device_id,omitempty"`
	Details  string `json:"details,omitempty"`
}

type apiStatus struct {
	Version       string           `json:"version"`
	Commit        string           `json:"commit,omitempty"`
	Build         string           `json:"build,omitempty"`
	Uptime        string           `json:"uptime"`
	Backend       string           `json:"backend"`
	Devices       int              `json:"devices"`
	DevicesOnline int              `json:"devices_online"`
	Subnets       int              `json:"subnets"`
	Integrations  []apiIntegration `json:"integrations"`
}

type apiIntegration struct {
	Name    string `json:"name"`
	LastRun string `json:"last_run"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
}

// writeJSON sends v as JSON. An encoding failure after the status line is
// already out cannot be turned into an error response, so it is logged.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing json response", "path", r.URL.Path, "err", err)
	}
}

func (s *Server) handleAPIDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]apiDevice, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIDevice(row))
	}
	s.writeJSON(w, r, map[string]any{"devices": out})
}

func (s *Server) handleAPIDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// ListDevices is the only read that assembles a whole device row with its
	// IPs, MACs and tags, and it costs a fixed five queries regardless of size.
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, row := range rows {
		if row.ID == id {
			s.writeJSON(w, r, toAPIDevice(row))
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) handleAPISubnets(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]apiSubnet, 0, len(subnets))
	for _, sn := range subnets {
		out = append(out, apiSubnet{
			ID: sn.ID, CIDR: sn.CIDR, Name: sn.Name, Kind: sn.Kind, VLANID: sn.VLANID,
			ScanEnabled: sn.ScanEnabled, ScanIntervalSec: sn.ScanIntervalSec,
		})
	}
	s.writeJSON(w, r, map[string]any{"subnets": out})
}

// apiEventLimit bounds ?limit= so one request cannot ask netis to marshal the
// entire event history into memory.
const apiEventLimit = 1000

func (s *Server) handleAPIEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		limit = min(n, apiEventLimit)
	}
	events, err := s.store.ListEvents(r.Context(), limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]apiEvent, 0, len(events))
	for _, e := range events {
		out = append(out, apiEvent{ID: e.ID, TS: e.TS, Type: e.Type, DeviceID: e.DeviceID, Details: e.Details})
	}
	s.writeJSON(w, r, map[string]any{"events": out})
}

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	online := 0
	for _, row := range rows {
		if row.Online {
			online++
		}
	}
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	statuses, err := s.store.ListIntegrationStatus(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ints := make([]apiIntegration, 0, len(statuses))
	for _, it := range statuses {
		ints = append(ints, apiIntegration{Name: it.Name, LastRun: it.LastRun, OK: it.OK, Detail: it.Detail})
	}
	info := buildinfo.Get()
	s.writeJSON(w, r, apiStatus{
		Version: info.Version, Commit: info.Commit, Build: info.Build,
		Uptime: buildinfo.Uptime().String(), Backend: string(s.store.Dialect()),
		Devices: len(rows), DevicesOnline: online, Subnets: len(subnets),
		Integrations: ints,
	})
}
