package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"netis/internal/store"
)

// The API's device writes. They are admin-only (wrapped in requireAdmin at
// registration) and validate exactly what the HTML forms validate, sharing
// checkNewDevice with the create form, so a script cannot put into the
// inventory anything a person at the UI could not.

// apiDeviceCreate is the body of POST /api/devices.
type apiDeviceCreate struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Notes    string   `json:"notes"`
	Vendor   string   `json:"vendor"`
	Model    string   `json:"model"`
	Function string   `json:"function"`
	Icon     string   `json:"icon"`
	ParentID *int64   `json:"parent_device_id"`
	Tags     []string `json:"tags"`
	MAC      string   `json:"mac"`
	IP       string   `json:"ip"`
	SubnetID int64    `json:"subnet_id"`
}

// decodeJSONBody reads a JSON request body into v, refusing unknown fields
// so a misspelt one is an error rather than silently dropped. It answers the
// request itself and returns false when the body is unusable.
func (s *Server) decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		s.apiError(w, r, http.StatusUnsupportedMediaType, "send the body as application/json")
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.apiError(w, r, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		s.apiError(w, r, http.StatusBadRequest, "malformed JSON: "+err.Error())
		return false
	}
	return true
}

// apiDeviceByID assembles one device in the API shape.
func (s *Server) apiDeviceByID(ctx context.Context, id int64) (apiDevice, bool, error) {
	// ListDevices is the only read that assembles a whole device row with its
	// IPs, MACs and tags, and it costs a fixed five queries regardless of size.
	rows, err := s.store.ListDevices(ctx)
	if err != nil {
		return apiDevice{}, false, err
	}
	for _, row := range rows {
		if row.ID == id {
			return toAPIDevice(row), true, nil
		}
	}
	return apiDevice{}, false, nil
}

// subnetContaining returns the id of the configured subnet that contains ip,
// or zero when ip does not parse or no subnet holds it. The narrowest match
// wins, so a /24 inside a /16 is preferred.
func (s *Server) subnetContaining(ctx context.Context, ip string) (int64, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return 0, nil
	}
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return 0, err
	}
	var best int64
	bits := -1
	for _, sn := range subnets {
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil || !p.Contains(addr) {
			continue
		}
		if p.Bits() > bits {
			best, bits = sn.ID, p.Bits()
		}
	}
	return best, nil
}

func (s *Server) handleAPIDeviceCreate(w http.ResponseWriter, r *http.Request) {
	var in apiDeviceCreate
	if !s.decodeJSONBody(w, r, &in) {
		return
	}
	auditNote(r).Target = "device " + in.Name
	dev := store.Device{
		Name: in.Name, Kind: in.Kind, Notes: in.Notes, Icon: in.Icon, Source: "manual",
		Vendor: in.Vendor, Model: in.Model, Function: in.Function, ParentDeviceID: in.ParentID,
	}
	// A script names an address, not a subnet row; find the subnet for it
	// when none was given. The form asks, because a person can see the list.
	subnetID := in.SubnetID
	if subnetID == 0 && strings.TrimSpace(in.IP) != "" {
		id, err := s.subnetContaining(r.Context(), in.IP)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		subnetID = id
	}
	nd, msg, err := s.checkNewDevice(r.Context(), dev, in.MAC, in.IP, subnetID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if msg != "" {
		if msg == chooseSubnetMsg {
			msg = "no configured subnet contains " + strings.TrimSpace(in.IP) + "; pass subnet_id"
		}
		s.apiError(w, r, http.StatusBadRequest, msg)
		return
	}
	id, err := s.store.CreateDeviceWithIface(r.Context(), nd.dev, nd.mac, nd.subnetID, nd.ip, "static")
	if err != nil {
		if status, msg, ok := writeFailure(err, macTakenMsg, parentMissingMsg); ok {
			s.apiError(w, r, status, msg)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetDeviceTags(r.Context(), id, in.Tags); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeDevice(w, r, id, http.StatusCreated)
}

// writeDevice answers with the device in the GET /api/devices/{id} shape.
func (s *Server) writeDevice(w http.ResponseWriter, r *http.Request, id int64, status int) {
	d, ok, err := s.apiDeviceByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.apiError(w, r, http.StatusNotFound, "device not found")
		return
	}
	if status == http.StatusCreated {
		w.Header().Set("Location", "/api/devices/"+strconv.FormatInt(id, 10))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	s.writeJSON(w, r, d)
}

// apiDeviceID reads the {id} path value, answering 404 for one that is not a
// number or names no device.
func (s *Server) apiDeviceID(w http.ResponseWriter, r *http.Request) (store.Device, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.apiError(w, r, http.StatusNotFound, "device not found")
		return store.Device{}, false
	}
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.apiError(w, r, http.StatusNotFound, "device not found")
			return store.Device{}, false
		}
		s.fail(w, r, err)
		return store.Device{}, false
	}
	return d, true
}

// patchable lists the fields PATCH /api/devices/{id} accepts.
var patchable = map[string]bool{
	"name": true, "kind": true, "notes": true, "vendor": true, "model": true,
	"function": true, "icon": true, "parent_device_id": true, "tags": true,
}

// handleAPIDeviceUpdate changes the fields named in the body and leaves the
// rest alone. The body is read as raw fields so "absent" and "null" differ:
// "parent_device_id": null clears the parent, leaving it out keeps it.
func (s *Server) handleAPIDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.apiDeviceID(w, r)
	if !ok {
		return
	}
	auditNote(r).Target = deviceTarget(d)
	var body map[string]json.RawMessage
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	for k := range body {
		if !patchable[k] {
			s.apiError(w, r, http.StatusBadRequest, "unknown or read-only field "+strconv.Quote(k))
			return
		}
	}
	str := func(key string, dst *string) bool {
		raw, ok := body[key]
		if !ok {
			return true
		}
		if err := json.Unmarshal(raw, dst); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			s.apiError(w, r, http.StatusBadRequest, key+" must be a string")
			return false
		}
		return true
	}
	for key, dst := range map[string]*string{
		"name": &d.Name, "kind": &d.Kind, "notes": &d.Notes, "vendor": &d.Vendor,
		"model": &d.Model, "function": &d.Function, "icon": &d.Icon,
	} {
		if !str(key, dst) {
			return
		}
	}
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" {
		s.apiError(w, r, http.StatusBadRequest, "name required")
		return
	}
	if !validKinds[d.Kind] {
		s.apiError(w, r, http.StatusBadRequest, "bad kind")
		return
	}
	if raw, ok := body["parent_device_id"]; ok {
		var pid *int64
		if err := json.Unmarshal(raw, &pid); err != nil {
			s.apiError(w, r, http.StatusBadRequest, "parent_device_id must be a device id or null")
			return
		}
		if pid != nil && *pid == d.ID {
			s.apiError(w, r, http.StatusBadRequest, "a device cannot be its own parent")
			return
		}
		d.ParentDeviceID = pid
	}
	var tags []string
	_, setTags := body["tags"]
	if setTags {
		if err := json.Unmarshal(body["tags"], &tags); err != nil {
			s.apiError(w, r, http.StatusBadRequest, "tags must be a list of strings")
			return
		}
	}
	if err := s.store.UpdateDevice(r.Context(), d); err != nil {
		if status, msg, ok := writeFailure(err, "device conflicts with an existing one", parentMissingMsg); ok {
			s.apiError(w, r, status, msg)
			return
		}
		s.fail(w, r, err)
		return
	}
	if setTags {
		if err := s.store.SetDeviceTags(r.Context(), d.ID, tags); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.writeDevice(w, r, d.ID, http.StatusOK)
}

func (s *Server) handleAPIDeviceDelete(w http.ResponseWriter, r *http.Request) {
	d, ok := s.apiDeviceID(w, r)
	if !ok {
		return
	}
	auditNote(r).Target = deviceTarget(d)
	if err := s.store.DeleteDevice(r.Context(), d.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
